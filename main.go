package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
	"gopkg.in/yaml.v2"
)

type Args struct {
	Mode   *string
	Name   *string
	LogDir *string
}
type Log struct {
	Type string `yaml:"type"`
	NS   string `yaml:"namespace"`
	Pod  string `yaml:"pod"`
	Name string `yaml:"name"`
	Dir  string `yaml:"dir"`
	File string `yaml:"file"`
}
type HostInfo struct {
	IP       string `yaml:"ip"`
	Port     int    `yaml:"port"`
	User     string `yaml:"user"`
	Password string `yaml:"password"`
}
type Config struct {
	Hosts []HostInfo `yaml:"host"`
	Logs  []Log      `yaml:"logs"`
}
type Charset string

const (
	UTF8    = Charset("UTF-8")
	GB18030 = Charset("GB18030")
)

func strip(s_ string, chars_ string) string {
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

// Run
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

	if err != nil {
		msg := fmt.Sprintf("ERROR: cmd(%v), err(%v)", command, ConvertByte2String(result, "GB18030"))
		log.Println(msg)
	}
	resultFormat := strip(string(result), "\n")
	return resultFormat, err
}

// ConvertByte2String
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

func ReadYamlConfig(path string) (*Config, error) {
	conf := &Config{}
	if f, err := os.Open(path); err != nil {
		return nil, err
	} else {
		yaml.NewDecoder(f).Decode(conf)
	}
	return conf, nil
}

func getLogNameList(conf Config, name string) Log {
	for _, logItem := range conf.Logs {
		if logItem.Name == name {
			return logItem
		}
	}
	return Log{}
}
func checkFileLink() {

}
func getAllPod(conf Log) []string {
	cmdStr := fmt.Sprintf("kubectl -n %v get pod |grep '%v'|awk '{print $1}'", conf.NS, conf.Pod)
	result, err := Run(cmdStr)
	if err != nil {
		return []string{}
	}

	podName := strings.Split(result, "\n")
	return podName

}
func tarFIle(src, dest string) {
	cmdStr := fmt.Sprintf("tar zcf %v.tar.gz %v --remove-files", src, src)
	if _, err := Run(cmdStr); err != nil {
		log.Println("ERROR: zip log ", err)
	}
}

func fetchLogFile(conf Log, arg Args) {
	// kubectl -n sso cp mariadb-sso-test-ss-0:/workspace/agent  ./agent
	destDir := fmt.Sprintf("%v/%v", *arg.LogDir, conf.Name)
	if _, err := Run("mkdir -p " + destDir); err != nil {
		log.Fatalln(err)
	}
	podNameList := getAllPod(conf)
	for _, podName := range podNameList {
		logFilePath := conf.Dir + "/" + conf.File
		if checkSpace(conf, arg, logFilePath, podName) {
			cmdStr := fmt.Sprintf("kubectl -n %v cp %v:%v %v/%v.log", conf.NS, podName, logFilePath, destDir, podName)
			if _, err := Run(cmdStr); err != nil {
				log.Println("ERROR: fetch log ", podNameList, podName)
			}
		} else {
			log.Println("ERROR: disk + logfile must < 85%")
		}
	}
	tarFIle(destDir, conf.Name)
	log.Println(fmt.Sprintf("INFO logfile path: %v.tar.gz", destDir))

}
func getFileSize(conf Log, logfie, pod string) string {
	cmdStr := fmt.Sprintf("kubectl -n %v exec -i %v -- bash -c \"du -k %v|awk '{print \\$1}'\" ", conf.NS, pod, logfie)
	result, err := Run(cmdStr)
	if err != nil {
		log.Fatalln("get disk info failed")
	}
	return result
}
func CurDiskInfo(path string) []string {
	Run("mkdir -p " + path)
	cmdStr := "df  " + path + "|awk 'NR>1{print $2,$3}'"
	result, err := Run(cmdStr)
	if err != nil {
		log.Fatalln("get disk info failed")
	}
	diskInfo := strings.Split(result, " ")
	return diskInfo
}
func checkSpace(conf Log, arg Args, logfie, pod string) bool {
	diskInfo := CurDiskInfo(*arg.LogDir)
	fileSizeStr := getFileSize(conf, logfie, pod)
	fileSize, _ := strconv.ParseInt(fileSizeStr, 10, 64)
	diskAll, _ := strconv.ParseInt(diskInfo[0], 10, 64)
	diskUsed, _ := strconv.ParseInt(diskInfo[1], 10, 64)

	if (fileSize+diskUsed)/diskAll*100 < 85 {
		return true
	}
	return false

}
func main() {
	arg := Args{}

	arg.Mode = flag.String("m", "", "list all log name")
	arg.Name = flag.String("n", "", "log name")
	arg.LogDir = flag.String("d", "/tmp/logs", "dest logs dir")
	flag.Parse()

	log.SetFlags(log.Lshortfile | log.LstdFlags)
	conf, err := ReadYamlConfig("conf.yml")
	if err != nil {
		log.Fatal(err)
	}
	if *arg.Mode == "get" {
		logInfo := getLogNameList(*conf, *arg.Name)
		if logInfo.Name == "" {
			log.Println("not found log: ", *arg.Name)
		} else {
			fetchLogFile(logInfo, arg)
		}

	} else if *arg.Mode == "list" {
		for _, logItem := range conf.Logs {
			fmt.Println(logItem.Name)
		}
		fmt.Println("")
		fmt.Println("Usage: ./log-collect -m get -n xxx")
	} else {
		log.Println("Usage: ./log-collect -m get/list")
	}
}
