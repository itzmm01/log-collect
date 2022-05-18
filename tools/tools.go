package tools

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"

	"golang.org/x/text/encoding/simplifiedchinese"
)

type Charset string

var DEBUG bool

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
		log.Println(command)
	}

	if err != nil {
		msg := fmt.Sprintf("ERROR: cmd(%v), err(%v)", command, ConvertByte2String(result, "GB18030"))
		log.Println(msg)
	}
	resultFormat := Strip(string(result), "\n")
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
