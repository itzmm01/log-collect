package tools

import (
	"archive/tar"
	"compress/gzip"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path"
	"runtime"
	"strings"
	"time"

	"github.com/juju/ratelimit"
	"golang.org/x/text/encoding/simplifiedchinese"
)

type Charset string

var DEBUG bool
var Limit = 0

const (
	UTF8    = Charset("UTF-8")
	GB18030 = Charset("GB18030")
)

func Strip(s_ string, chars_ string) string {
	s, chars := []rune(s_), []rune(chars_)
	length := len(s)
	max := len(s) - 1
	l, r := true, true //标记当左端或者右端找到正常字符后就停止继续寻找
	start, end := 0, max
	tmpEnd := 0
	charset := make(map[rune]bool) //创建字符集，也就是唯一的字符，方便后面判断是否存在
	for i := 0; i < len(chars); i++ {
		charset[chars[i]] = true
	}
	for i := 0; i < length; i++ {
		if _, exist := charset[s[i]]; l && !exist {
			start = i
			l = false
		}
		tmpEnd = max - i
		if _, exist := charset[s[tmpEnd]]; r && !exist {
			end = tmpEnd
			r = false
		}
		if !l && !r {
			break
		}
	}
	if l && r { // 如果左端和右端都没找到正常字符，那么表示该字符串没有正常字符
		return ""
	}
	return string(s[start : end+1])
}

// Run cmd
func Run(command string) (string, error) {
	var result []byte
	var err error

	sysType := runtime.GOOS

	if sysType == "windows" {
		result, err = exec.Command("cmd", "/c", command).CombinedOutput()
		// logger.Error("no support system: ", sysType)
	} else if sysType == "linux" {
		result, err = exec.Command("/bin/sh", "-c", command).CombinedOutput()
	} else {
		log.Println("no support system: ", sysType)
	}

	if DEBUG {
		log.Println(command, string(result))
	}

	resultFormat := Strip(ConvertByte2String(result, "GB18030"), "\n")
	return resultFormat, err
}

// ConvertByte2String Solve Chinese garbled code
func ConvertByte2String(byte []byte, charset Charset) string {
	var str string
	switch charset {
	case GB18030:
		var decodeBytes, _ = simplifiedchinese.GB18030.NewDecoder().Bytes(byte)
		str = string(decodeBytes)
	case UTF8:
		fallthrough
	default:
		str = string(byte)
	}
	return str
}
func CurDiskInfo(path string) []string {
	if _, err := Mkdir(path); err != nil {
		log.Fatalln("[ERROR] get cur disk  info failed")
	}
	cmdStr := "df  " + path + "|awk 'NR>1{print $2,$3}'"
	result, err := Run(cmdStr)
	if err != nil {
		log.Fatalln("get disk info failed")
	}
	diskInfo := strings.Split(result, " ")
	return diskInfo
}

type NewError struct {
	Msg string
}

//NewError Error() 方法的对象都可以
func (e *NewError) Error() string {
	return e.Msg
}

func PathExists(path string) bool {
	_, err := os.Stat(path)
	if err == nil {
		return true
	}
	if os.IsNotExist(err) {
		return false
	}
	return false
}

func Mkdir(path string) (bool, error) {
	err := os.MkdirAll(path, 0755)
	if err != nil {
		return false, err
	} else {
		return true, nil
	}
}

func LimitDownload(reader io.Reader, destDir string) error {
	dstFile, _ := os.Create(destDir)
	var bucket *ratelimit.Bucket
	if Limit == 0 {
		// max 10G ~= unlimited
		bucket = ratelimit.NewBucketWithRate(10000*1024000, 10000*1024000)
	} else {
		// Bucket adding limit MB every second, limit MB
		bucket = ratelimit.NewBucketWithRate(float64(Limit*1024000), int64(Limit*1024000))
	}
	_, err := io.Copy(dstFile, ratelimit.Reader(reader, bucket))
	if err != nil {
		return err
	}
	defer func() {
		_ = dstFile.Close()
	}()
	// 防止本次IO还未完成,进行下一轮IO
	time.Sleep(1000 * time.Millisecond)
	return nil
}

func DeleteDir(localPath string) {
	dir, _ := ioutil.ReadDir(localPath)
	for _, d := range dir {
		os.RemoveAll(path.Join([]string{localPath, d.Name()}...))
	}
	os.RemoveAll(localPath)
}

// Compress Compress tar.gz
func Compress(files []string, dest string) error {
	d, _ := os.Create(dest)
	defer d.Close()
	gw := gzip.NewWriter(d)
	defer gw.Close()
	tw := tar.NewWriter(gw)
	defer tw.Close()
	for _, file := range files {
		srcFile, _ := os.Open(file)
		err := compress(srcFile, "", tw)
		if err != nil {
			return err
		}
		DeleteDir(file)
	}
	return nil
}

func compress(file *os.File, prefix string, tw *tar.Writer) error {
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.IsDir() {
		prefix = prefix + "/" + info.Name()
		fileInfos, err := file.Readdir(-1)
		if err != nil {
			return err
		}
		for _, fi := range fileInfos {
			f, err := os.Open(file.Name() + "/" + fi.Name())
			if err != nil {
				return err
			}
			err = compress(f, prefix, tw)
			if err != nil {
				return err
			}
		}
	} else {
		header, err := tar.FileInfoHeader(info, "")
		header.Name = prefix + "/" + header.Name
		if err != nil {
			return err
		}
		err = tw.WriteHeader(header)
		if err != nil {
			return err
		}
		var bucket *ratelimit.Bucket
		if Limit == 0 {
			// max 10G ~= unlimited
			bucket = ratelimit.NewBucketWithRate(10000*1024000, 10000*1024000)
		} else {
			// Bucket adding limit MB every second, limit MB
			bucket = ratelimit.NewBucketWithRate(float64(Limit*1024000), int64(Limit*1024000))
		}

		//_, err = io.Copy(tw, file)
		_, err = io.Copy(tw, ratelimit.Reader(file, bucket))
		file.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// DeCompress 解压 tar.gz
func DeCompress(tarFile, dest string) error {
	srcFile, err := os.Open(tarFile)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	gr, err := gzip.NewReader(srcFile)
	if err != nil {
		return err
	}
	defer gr.Close()
	tr := tar.NewReader(gr)
	for {
		hdr, err := tr.Next()
		if err != nil {
			if err == io.EOF {
				break
			} else {
				return err
			}
		}
		filename := dest + hdr.Name
		file, err := createFile(filename)
		if err != nil {
			return err
		}
		io.Copy(file, tr)
	}
	return nil
}

func createFile(name string) (*os.File, error) {
	err := os.MkdirAll(string([]rune(name)[0:strings.LastIndex(name, "/")]), 0755)
	if err != nil {
		return nil, err
	}
	return os.Create(name)
}

func KubectlLogs(ns, podPreFix, container, num, destDir string) error {
	getPodCmd := fmt.Sprintf("kubectl -n %s get pod|grep '%s'|awk '{print $1}'", ns, podPreFix)
	allPodStr, err := Run(getPodCmd)
	if err != nil {
		return &NewError{Msg: allPodStr}
	}
	if allPodStr == "" {
		return &NewError{Msg: "Pod not found"}
	}
	podList := strings.Split(allPodStr, "\n")
	for _, pod := range podList {
		destFile := fmt.Sprintf("%s/%s-pod.log", destDir, pod)
		log.Println(fmt.Sprintf("[INFO] Download %s to %s ", pod, destFile))
		var cmd string
		if container != "" {
			cmd = fmt.Sprintf("kubectl -n %s logs --tail %s %s -c %s > %s", ns, num, pod, container, destFile)
		} else {
			cmd = fmt.Sprintf("kubectl -n %s logs --tail %s %s > %s", ns, num, pod, destFile)
		}
		_, err := Run(cmd)
		if err != nil {
			return err
		}
	}

	return nil
}
