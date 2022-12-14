package k8s

import (
	"archive/tar"
	"bufio"
	"bytes"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"log-collect/tools"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/juju/ratelimit"
	coreV1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	cmdUtil "k8s.io/kubectl/pkg/cmd/util"
)

var (
	KubeQPS            = float32(5.000000)
	KubeBurst          = 10
	kubeConfig         *string
	AcceptContentTypes = "application/json"
	ContentType        = "application/json"
)

func setKubeConfig(config *rest.Config) {
	config.QPS = KubeQPS
	config.Burst = KubeBurst
	config.ContentType = ContentType
	config.AcceptContentTypes = AcceptContentTypes
	config.UserAgent = rest.DefaultKubernetesUserAgent()
}

// InitKubeConfig 初始化 k8s api 连接配置
func InitKubeConfig(env bool) (*rest.Config, error) {

	if !env {
		if kubeConfig != nil {
			config, err := clientcmd.BuildConfigFromFlags("", *kubeConfig)
			if err != nil {
				panic(err.Error())
			}
			setKubeConfig(config)
			return config, err
		}
		defaultConfig := "/root/.kube/config"
		if tools.PathExists(defaultConfig) {
			kubeConfig = &defaultConfig
		} else {
			defaultConfig = "./config"
		}
		kubeConfig = &defaultConfig
		config, err := clientcmd.BuildConfigFromFlags("", *kubeConfig)
		if err != nil {
			panic(err.Error())
		}
		setKubeConfig(config)
		return config, err

	} else {
		config, err := rest.InClusterConfig()
		if err != nil {
			panic(err.Error())
		}

		if err != nil {
			panic(err)
		}
		setKubeConfig(config)
		return config, err
	}
}

// NewClientSet ClientSet 客户端
func NewClientSet(c *rest.Config) (*kubernetes.Clientset, error) {
	clientSet, err := kubernetes.NewForConfig(c)
	return clientSet, err
}

func getPrefix(file string) string {
	return strings.TrimLeft(file, "/")
}

// stripPathShortcuts removes any leading or trailing "../" from a given path
func stripPathShortcuts(p string) string {

	newPath := path.Clean(p)
	trimmed := strings.TrimPrefix(newPath, "../")

	for trimmed != newPath {
		newPath = trimmed
		trimmed = strings.TrimPrefix(newPath, "../")
	}

	// trim leftover {".", ".."}
	if newPath == "." || newPath == ".." {
		newPath = ""
	}

	if len(newPath) > 0 && string(newPath[0]) == "/" {
		return newPath[1:]
	}

	return newPath
}
func unTarAll(reader io.Reader, destDir, prefix string) error {
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err != nil {
			if err != io.EOF {
				return err
			}
			break
		}

		if !strings.HasPrefix(header.Name, prefix) {
			return fmt.Errorf("tar contents corrupted")
		}

		mode := header.FileInfo().Mode()
		destFileName := filepath.Join(destDir, header.Name[len(prefix):])

		baseName := filepath.Dir(destFileName)
		if err := os.MkdirAll(baseName, 0755); err != nil {
			return err
		}
		if header.FileInfo().IsDir() {
			if err := os.MkdirAll(destFileName, 0755); err != nil {
				return err
			}
			continue
		}

		evalPath, err := filepath.EvalSymlinks(baseName)
		if err != nil {
			return err
		}

		if mode&os.ModeSymlink != 0 {
			linkname := header.Linkname

			if !filepath.IsAbs(linkname) {
				_ = filepath.Join(evalPath, linkname)
			}

			if err := os.Symlink(linkname, destFileName); err != nil {
				return err
			}
		} else {
			outFile, err := os.Create(destFileName)
			if err != nil {
				return err
			}
			defer outFile.Close()

			if _, err := io.Copy(outFile, tarReader); err != nil {
				return err
			}
			if err := outFile.Close(); err != nil {
				return err
			}
		}
	}
	return nil
}

// unTarAll1 4K per read
func unTarAll1(reader io.Reader, destDir string, limit int) error {
	buf := bufio.NewReader(reader)
	outFile, _ := os.Create(destDir)
	w := bufio.NewWriter(outFile)
	s := make([]byte, 40960)

	// Bucket adding limit MB every second, limit MB
	bucket := ratelimit.NewBucketWithRate(float64(limit*1024000), int64(limit*1024000))
	if limit == 0 {
		// max 10G ~= unlimited
		bucket = ratelimit.NewBucketWithRate(10000*1024000, 10000*1024000)
	}

	start := time.Now()
	for {
		_, err := buf.Read(s)
		if err != nil {
			if err != io.EOF {
				return err
			}
			break
		}
		// Copy source to destination, but wrap our reader with rate limited one
		io.Copy(w, ratelimit.Reader(reader, bucket))
	}
	log.Println("Copied in ", time.Since(start))
	defer outFile.Close()
	return nil
}

// CopyFromPod 从 pod 复制文件到本地
func CopyFromPod(r *rest.Config, c *kubernetes.Clientset, pod, ns, srcPathStr, dest, container string, isTar bool) error {
	reader, outStream := io.Pipe()
	srcPathList := strings.Split(srcPathStr, "/")
	srcPath := ""
	srcFile := ""
	if srcPathList[len(srcPathList)-1] == "" {
		srcPath = strings.Join(srcPathList[0:len(srcPathList)-2], "/")
		srcFile = srcPathList[len(srcPathList)-2]
	} else {
		srcPath = strings.Join(srcPathList[0:len(srcPathList)-1], "/")
		srcFile = srcPathList[len(srcPathList)-1]
	}
	var cmd []string
	if isTar {
		cmd = []string{"tar", "cf", "-", srcPathStr, "--warning=no-file-changed"}
	} else {
		if srcPathList[len(srcPathList)-1] == "" {
			cmd := "ls " + srcPathStr
			res, err := Exec(r, c, pod, ns, cmd, container)
			if err != nil {
				return &tools.NewError{Msg: res}
			}
			msg := "There is no tar command in the container. Directories are not supported: " + srcPathStr
			return &tools.NewError{Msg: msg}
		}
		cmd = []string{"cat", srcPathStr}
	}
	destPath := dest + "/" + pod
	tools.Mkdir(destPath)
	var destFile string
	if cmd[0] == "cat" {
		destFile = destPath + "/" + srcFile
	} else {
		destFile = destPath + "/" + srcFile + ".tar.gz"
	}
	msg := fmt.Sprintf("[INFO] Download %s/%s %s", srcPath, srcFile, destFile)
	log.Println(msg)
	// 初始化pod所在的 coreV1 资源组，发送请求
	req := c.CoreV1().RESTClient().Get().
		Resource("pods").
		Name(pod).
		Namespace(ns).
		SubResource("exec").
		VersionedParams(&coreV1.PodExecOptions{
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
			Container: container,
			// 将数据转换成数据流
			Command: cmd,
		}, scheme.ParameterCodec)

	// remote-command 主要实现了http 转 SPDY 添加X-Stream-Protocol-Version相关header 并发送请求
	exec, err := remotecommand.NewSPDYExecutor(r, "POST", req.URL())
	if err != nil {
		return err
	}
	go func() {
		defer outStream.Close()
		var errStream bytes.Buffer
		err = exec.Stream(remotecommand.StreamOptions{
			Stdin:  os.Stdin,
			Stdout: outStream,
			Stderr: &errStream,
			Tty:    false,
		})
		if err != nil {
			log.Println("[ERROR]: ", errStream.String())
		}
	}()

	err = tools.LimitDownload(reader, destFile)
	if err != nil {
		return err
	}

	return nil
}

func CopyToPod(r *rest.Config, c *kubernetes.Clientset) error {
	reader, writer := io.Pipe()
	go func() {
		defer writer.Close()
		cmdUtil.CheckErr(makeTar("./demo", "/demo", writer))
	}()

	req := c.CoreV1().RESTClient().Post().
		Resource("pods").
		Name("nginx-6fc95cbdfc-dlnt6").
		Namespace("default").
		SubResource("exec").
		VersionedParams(&coreV1.PodExecOptions{
			Command: []string{"tar", "-xmf", "-"},
			Stdin:   true,
			Stdout:  true,
			Stderr:  true,
			TTY:     false,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(r, "POST", req.URL())
	if err != nil {
		return err
	}

	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  reader,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Tty:    false,
	})
	if err != nil {
		return err
	}
	return nil
}

func makeTar(srcPath, destPath string, writer io.Writer) error {
	// TODO: use compression here?
	tarWriter := tar.NewWriter(writer)
	defer tarWriter.Close()

	srcPath = path.Clean(srcPath)
	destPath = path.Clean(destPath)
	return recursiveTar(path.Dir(srcPath), path.Base(srcPath), path.Dir(destPath), path.Base(destPath), tarWriter)
}

func recursiveTar(srcBase, srcFile, destBase, destFile string, tw *tar.Writer) error {
	srcPath := path.Join(srcBase, srcFile)
	matchedPaths, err := filepath.Glob(srcPath)
	if err != nil {
		return err
	}
	for _, fpath := range matchedPaths {
		stat, err := os.Lstat(fpath)
		if err != nil {
			return err
		}
		if stat.IsDir() {
			files, err := ioutil.ReadDir(fpath)
			if err != nil {
				return err
			}
			if len(files) == 0 {
				//case empty directory
				hdr, _ := tar.FileInfoHeader(stat, fpath)
				hdr.Name = destFile
				if err := tw.WriteHeader(hdr); err != nil {
					return err
				}
			}
			for _, f := range files {
				if err := recursiveTar(srcBase, path.Join(srcFile, f.Name()), destBase, path.Join(destFile, f.Name()), tw); err != nil {
					return err
				}
			}
			return nil
		} else if stat.Mode()&os.ModeSymlink != 0 {
			//case soft link
			hdr, _ := tar.FileInfoHeader(stat, fpath)
			target, err := os.Readlink(fpath)
			if err != nil {
				return err
			}

			hdr.Linkname = target
			hdr.Name = destFile
			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}
		} else {
			//case regular file or other file type like pipe
			hdr, err := tar.FileInfoHeader(stat, fpath)
			if err != nil {
				return err
			}
			hdr.Name = destFile

			if err := tw.WriteHeader(hdr); err != nil {
				return err
			}

			f, err := os.Open(fpath)
			if err != nil {
				return err
			}
			defer f.Close()

			if _, err := io.Copy(tw, f); err != nil {
				return err
			}
			return f.Close()
		}
	}
	return nil
}
func Exec(r *rest.Config, c *kubernetes.Clientset, podName, namespace, cmd, container string) (string, error) {
	// 构造执行命令请求
	req := c.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&coreV1.PodExecOptions{
			Container: container,
			Command:   []string{"sh", "-c", cmd},
			Stdin:     true,
			Stdout:    true,
			Stderr:    true,
			TTY:       false,
		}, scheme.ParameterCodec)
	// 执行命令
	executor, err := remotecommand.NewSPDYExecutor(r, "POST", req.URL())
	if err != nil {
		log.Println(namespace, podName, cmd, err)
		return "", err
	}
	// 使用bytes.Buffer变量接收标准输出和标准错误
	var stdout, stderr bytes.Buffer
	if err = executor.Stream(remotecommand.StreamOptions{
		Stdin:  strings.NewReader(""),
		Stdout: &stdout,
		Stderr: &stderr,
	}); err != nil {
		return stderr.String(), err
	}
	result := stdout.String()
	if tools.DEBUG {
		log.Println(cmd, result)
	}
	return result, nil
}
