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
		log.Println("run cmd failed: ", err, ConvertByte2String(result, "GB18030"))
	}
	return string(result), err
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

func fetchLogFile(conf Log) {
	// kubectl -n sso cp mariadb-sso-test-ss-0:/workspace/agent  ./agent
	cmdStr := fmt.Sprintf("kubectl -n %v cp %v:%v/%v %v", conf.NS, conf.Pod, conf.Dir, conf.File, conf.File)
	if _, err := Run(cmdStr); err != nil {
		log.Fatalln(err)
	}

}
func getFileSize(conf Log) string {
	cmdStr := fmt.Sprintf("kubectl -n %v exec -i %v -- bash -c \"du -k %v/%v|awk '{print \\$1}'\" ", conf.NS, conf.Pod, conf.Dir, conf.File)
	result, err := Run(cmdStr)
	if err != nil {
		log.Fatalln("get disk info failed")
	}
	return result
}
func CurDiskInfo() []string {
	cmdStr := "df  .|awk 'NR>1{print $2,$3}'"
	result, err := Run(cmdStr)
	if err != nil {
		log.Fatalln("get disk info failed")
	}
	diskInfo := strings.Split(result, " ")
	return diskInfo
}
func checkSpace(conf Log) bool {
	diskInfo := CurDiskInfo()
	fileSizeStr := getFileSize(conf)
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
	// arg.Name = flag.String("d", "./logs", "dest logs dir")
	flag.Parse()

	conf, err := ReadYamlConfig("conf.yml")
	if err != nil {
		log.Fatal(err)
	}
	log.Println(*arg.Mode, *arg.Name)
	if *arg.Mode == "get" {
		//  kubectl exec -i mvlog-server-0 -- bash -c "ls -l /data/spp_mvlog_server/data/wemeet_conn|awk 'NR>1{print \$5}'"
		logInfo := getLogNameList(*conf, *arg.Name)
		if logInfo.Name == "" {
			log.Println("not found log: ", *arg.Name)
		} else {
			if checkSpace(logInfo) {
				fetchLogFile(logInfo)
			} else {
				log.Fatalln("disk space not enough: used + logfile  < 85% ")
			}

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
