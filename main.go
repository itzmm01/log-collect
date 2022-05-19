package main

import (
	"context"
	"flag"
	"fmt"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"log"
	"log-collect/k8s"
	"log-collect/ssh"
	"log-collect/tools"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"gopkg.in/yaml.v2"
	metaV1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

var (
	clientSet  *kubernetes.Clientset
	kubeConfig *rest.Config
)

const sysType = runtime.GOOS

type Args struct {
	Mode   *string
	Name   *string
	LogDir *string
	Debug  bool
}
type Log struct {
	Type        string `yaml:"type"`
	NS          string `yaml:"namespace"`
	Pod         string `yaml:"pod"`
	Name        string `yaml:"name"`
	Dir         string `yaml:"dir"`
	File        string `yaml:"file"`
	HostGroup   string `yaml:"hostgroup"`
	Host        string `yaml:"host"`
	HostInfo    []HostInfo
	podNameList []string
}
type HostInfo struct {
	IP       string `yaml:"ip"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
	KeyFile  string `yaml:"keyfile"`
}
type HostGroup struct {
	Port     int        `yaml:"port"`
	User     string     `yaml:"user"`
	Password string     `yaml:"password"`
	Host     []HostInfo `yaml:"ips"`
}
type Config struct {
	HostGroups map[string]HostGroup `yaml:"host"`
	Logs       []Log                `yaml:"logs"`
}

func ReadYamlConfig(path string) (*Config, error) {
	conf := &Config{}
	if f, err := os.Open(path); err != nil {
		return nil, err
	} else {
		err := yaml.NewDecoder(f).Decode(conf)
		if err != nil {
			return nil, err
		}
	}
	return conf, nil
}

func (ctx Config) UpdateHosts() {
	allHost := HostGroup{}
	for name, group := range ctx.HostGroups {
		for index, host := range group.Host {
			if host.User == "" {
				host.User = group.User
			}
			if host.Port == 0 {
				host.Port = group.Port
			}
			if host.Password == "" {
				host.Password = group.Password
			}
			allHost.Host = append(allHost.Host, host)
			group.Host[index] = host
		}
		ctx.HostGroups[name] = group
	}
	ctx.HostGroups["all"] = allHost

}
func (ctx Config) getLogNameList(name string) Log {
	for _, logItem := range ctx.Logs {
		if logItem.Name == name {
			return logItem
		}
	}
	return Log{}
}
func (ctx Log) GetLogHost(conf Config) []HostInfo {
	if ctx.Type == "ssh" {
		ctx.HostInfo = append(ctx.HostInfo, conf.HostGroups[ctx.HostGroup].Host...)
	}
	return ctx.HostInfo
}
func (ctx Log) GetAllPod() []string {
	pods, err := clientSet.CoreV1().Pods(ctx.NS).List(context.TODO(), metaV1.ListOptions{})
	if err != nil {
		log.Fatalln(err)
	}
	for _, pod := range pods.Items {
		reg1 := regexp.MustCompile(fmt.Sprintf("^%v", ctx.Pod))
		if reg1 == nil {
			log.Fatalln("Regular expression error: ", fmt.Sprintf("^%v", ctx.Pod))
		}
		podMatch := reg1.FindAllStringSubmatch(pod.Name, -1)
		if len(podMatch) > 0 {
			ctx.podNameList = append(ctx.podNameList, pod.Name)
		}
	}
	return ctx.podNameList
}

func (ctx Log) checkFileLink(dir, pod string, host HostInfo) string {
	cmdStr := fmt.Sprintf("ls -ld %v|grep '^l'", dir)
	if ctx.Type == "k8s" {
		result, err := k8s.Exec(kubeConfig, clientSet, pod, ctx.NS, cmdStr)
		if err != nil {
			return dir
		}
		dirList := strings.Split(result, " ")
		dirLink := dirList[len(dirList)-1]
		return dirLink
	} else if ctx.Type == "ssh" {
		cli := ssh.SSH{
			Host:     host.IP,
			Port:     int64(host.Port),
			Username: host.User,
			Password: host.Password,
			KeyFile:  host.KeyFile,
		}
		cli.CreateClient()
		result, err := cli.RunShell(cmdStr)
		if err != nil {
			return dir
		}
		dirList := strings.Split(result, " ")
		dirLink := dirList[len(dirList)-1]
		return dirLink
	}
	return dir

}

func tarFIle(src, dest string) {
	cmdStr := fmt.Sprintf("cd %v && tar zcf ../%v.tar.gz ../%v --remove-files", src, dest, dest)
	if _, err := tools.Run(cmdStr); err != nil {
		log.Fatalln("[ERROR] zip log ", err)
	}
}
func initK8sClient() {
	var err error
	// 实例化 k8s 客户端
	kubeConfig, err = k8s.InitKubeConfig(false)
	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}
	clientSet, err = k8s.NewClientSet(kubeConfig)
	if err != nil {
		log.Fatalf("ERROR: %s", err)
	}
}

func (ctx Log) K8sFile(arg Args, destDir string) {
	for _, podName := range ctx.GetAllPod() {
		var err error
		newDir := ""
		if newDir, err = ctx.regToRealDir(podName, HostInfo{}); err != nil {
			log.Fatalln("[ERROR] ", err)
		}

		newFilePath := ""
		if newFilePath, err = ctx.regToRealFile(newDir, podName, HostInfo{}); err != nil {
			log.Println()
			log.Fatalln("[ERROR] ", newDir, ctx.File, err)
		}

		logPath := newDir + newFilePath
		logFilePath := ctx.checkFileLink(logPath, podName, HostInfo{})

		log.Printf("[INFO] Download %v - %v", logFilePath, fmt.Sprintf("%v/%v.log", destDir, podName))
		if ctx.checkSpace(arg, logFilePath, podName, HostInfo{}) {
			err := k8s.CopyFromPod(kubeConfig, clientSet, podName, ctx.NS, logFilePath, destDir)
			if err != nil {
				log.Printf("ERROR: %s", err)
			}
		} else {
			log.Println("ERROR: disk + logfile must < 85%")
		}
	}
}

func (ctx Log) SSHFile(arg Args, destDir string) {
	for _, host := range ctx.HostInfo {
		var err error
		newDir := ""
		if newDir, err = ctx.regToRealDir("", host); err != nil {
			log.Fatalln("[ERROR] ", err)
		}

		newFilePath := ""
		if newFilePath, err = ctx.regToRealFile(newDir, "", host); err != nil {
			log.Fatalln("[ERROR] ", newDir, newFilePath, ctx.Dir, ctx.File, err)
		}

		logPath := newDir + newFilePath
		logFilePath := ctx.checkFileLink(logPath, "", host)

		if ctx.checkSpace(arg, logFilePath, "", host) {
			cli := ssh.SSH{
				Host:     host.IP,
				Port:     int64(host.Port),
				Username: host.User,
				Password: host.Password,
				KeyFile:  host.KeyFile,
			}
			cli.CreateClient()
			log.Printf("[INFO] Download %v - %v", logFilePath, fmt.Sprintf("%v/%v.log", destDir, host.IP))
			err := cli.Download(logFilePath, fmt.Sprintf("%v/%v.log", destDir, host.IP))
			if err != nil {
				log.Printf("[ERROR] download failed %v\n", err)
			}

		} else {
			log.Println("[ERROR] disk + logfile must < 85%")
		}
	}
}

func (ctx Log) fetchLogFile(arg Args) {
	// kubectl -n sso cp mariadb-sso-test-ss-0:/workspace/agent  ./agent
	destDir := fmt.Sprintf("%v/%v", *arg.LogDir, ctx.Name)
	if _, err := tools.Mkdir(destDir); err != nil {
		log.Fatalln(err)
	}
	if ctx.Type == "k8s" {
		initK8sClient()
		ctx.K8sFile(arg, destDir)
	} else if ctx.Type == "ssh" {
		ctx.SSHFile(arg, destDir)
	}

	tarFIle(destDir, ctx.Name)
	log.Printf("[INFO] logfile path: %v.tar.gz", destDir)

}
func (ctx Log) getFileSize(namespace, logfile, pod string, host HostInfo) string {
	var result string
	var err error
	cmdStr := fmt.Sprintf("du -k %v|awk '{print \\$1}'", logfile)
	k8sCmdStr := fmt.Sprintf("kubectl -n %v exec -i %v -- bash -c \"%v\" ", namespace, pod, cmdStr)
	if ctx.Type == "k8s" {
		result, err = tools.Run(k8sCmdStr)
		if err != nil {
			log.Fatalln("get disk info failed")
		}
	} else {
		cli := ssh.SSH{
			Host:     host.IP,
			Port:     int64(host.Port),
			Username: host.User,
			Password: host.Password,
			KeyFile:  host.KeyFile,
		}
		cli.CreateClient()
		result, err = cli.RunShell(cmdStr)
	}

	return result
}

func (ctx Log) checkSpace(arg Args, logfile, pod string, host HostInfo) bool {

	if sysType == "windows" {
		log.Println("This check is not supported. Please make your own judgment")
		return true
	}
	var result string
	var err error
	cmdStr := fmt.Sprintf("du -k %v|awk '{print \\$1}'", logfile)
	if ctx.Type == "k8s" {
		cmdStr1 := fmt.Sprintf("du -k %v|awk '{print $1}'", logfile)
		result, err = k8s.Exec(kubeConfig, clientSet, pod, ctx.NS, cmdStr1)
		if err != nil {
			log.Fatalln("[ERROR] get disk info failed", cmdStr1, err)
		}
	} else {
		cli := ssh.SSH{
			Host:     host.IP,
			Port:     int64(host.Port),
			Username: host.User,
			Password: host.Password,
			KeyFile:  host.KeyFile,
		}
		cli.CreateClient()
		result, err = cli.RunShell(cmdStr)
	}

	fileSizeStr := result
	diskInfo := tools.CurDiskInfo(*arg.LogDir)

	fileSize, _ := strconv.ParseInt(fileSizeStr, 10, 64)
	diskAll, _ := strconv.ParseInt(diskInfo[0], 10, 64)
	diskUsed, _ := strconv.ParseInt(diskInfo[1], 10, 64)

	if (fileSize+diskUsed)/diskAll*100 < 85 {
		return true
	}
	return false

}

func (ctx Log) regToRealDir(pod string, host HostInfo) (string, error) {
	if ctx.Dir == "/" {
		return ctx.Dir, nil
	}
	pathList := strings.Split(ctx.Dir, "/")
	path := ""
	for index, reg := range pathList {
		if index == 0 {
			path = path + "/"
		} else {
			cmdStr := fmt.Sprintf("ls %v |grep -P '%v$'", path, reg)
			var result string
			var err error
			if ctx.Type == "k8s" {
				result, err = k8s.Exec(kubeConfig, clientSet, pod, ctx.NS, cmdStr)
				path = path + tools.Strip(result, "\n") + "/"
			} else {
				cli := ssh.SSH{
					Host:     host.IP,
					Port:     int64(host.Port),
					Username: host.User,
					Password: host.Password,
					KeyFile:  host.KeyFile,
				}
				cli.CreateClient()
				result, err = cli.RunShell(cmdStr)
				dirPath := strings.Split(result, "\n")
				path = path + dirPath[len(dirPath)-1] + "/"
			}
			if err != nil {
				return "", &tools.NewError{Msg: "not found " + reg}
			}
		}
	}
	return path, nil
}

func (ctx Log) regToRealFile(oldPath, pod string, host HostInfo) (string, error) {
	path := ""
	var result string
	var err error
	cmdStr := fmt.Sprintf("ls %v |grep -P '%v$'", oldPath, ctx.File)
	if ctx.Type == "k8s" {
		result, err = k8s.Exec(kubeConfig, clientSet, pod, ctx.NS, cmdStr)
		if err != nil {
			return "", &tools.NewError{Msg: fmt.Sprintf(cmdStr, result, err)}
		} else {
			return tools.Strip(result, "\n"), nil
		}
	} else {
		cli := ssh.SSH{
			Host:     host.IP,
			Port:     int64(host.Port),
			Username: host.User,
			Password: host.Password,
			KeyFile:  host.KeyFile,
		}
		cli.CreateClient()
		result, err = cli.RunShell(cmdStr)
		if err != nil {
			return "", &tools.NewError{Msg: fmt.Sprintf(cmdStr, result, err)}
		} else {
			dirPath := strings.Split(result, "\n")
			path = dirPath[len(dirPath)-1]
			return path, nil
		}
	}
}

func main() {
	arg := Args{}

	arg.Mode = flag.String("m", "", "list all log name")
	arg.Name = flag.String("n", "", "log name")
	arg.LogDir = flag.String("d", "/tmp/logs", "dest logs dir")
	arg.Debug = *flag.Bool("debug", false, "debug")
	flag.Parse()

	log.SetFlags(log.Lshortfile | log.LstdFlags)
	tools.DEBUG = arg.Debug
	conf, err := ReadYamlConfig("conf.yml")

	conf.UpdateHosts()

	if err != nil {
		log.Fatal(err)
	}
	if *arg.Mode == "get" {
		logInfo := conf.getLogNameList(*arg.Name)
		logInfo.HostInfo = logInfo.GetLogHost(*conf)
		if logInfo.Name == "" {
			log.Println("not found log: ", *arg.Name)
		} else {
			logInfo.fetchLogFile(arg)
		}

	} else if *arg.Mode == "list" {
		for _, logItem := range conf.Logs {
			fmt.Println(logItem.Name)
		}
		fmt.Println("----------------------------------")
		fmt.Println("Usage: ./log-collect -m get -n xxx")
	} else {
		log.Println("Usage: ./log-collect -m get/list")
	}
}
