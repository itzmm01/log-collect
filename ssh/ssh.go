package ssh

import (
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"log-collect/tools"
	"net"
	"os"
	"path"
	"strings"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// SSH struct
type SSH struct {
	Host       string       //ip
	Port       int64        // 端口
	Username   string       //用户名
	Password   string       //密码
	KeyFile    string       //密钥文件
	sshClient  *ssh.Client  //ssh client
	sftpClient *sftp.Client //sftp client
	LastResult string       //最近一次运行的结果
}

func publicKeyAuthFunc(keyPath string) ssh.AuthMethod {
	key, err := ioutil.ReadFile(keyPath)
	if err != nil {
		log.Println("[ERROR] Failed to read ssh key file:", err)
	}
	// Create the Signer for this private key.
	signer, err := ssh.ParsePrivateKey(key)
	if err != nil {
		log.Println("[ERROR] Failed to signature ssh key file: ", err)
	}
	return ssh.PublicKeys(signer)
}

// CreateClient Create SSH Client
func (cliConf *SSH) CreateClient() {
	var (
		sshClient  *ssh.Client
		sftpClient *sftp.Client
		err        error
	)

	config := ssh.ClientConfig{
		User: cliConf.Username,
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return nil
		},
		Timeout: 10 * time.Second,
	}
	if cliConf.KeyFile == "" {
		config.Auth = []ssh.AuthMethod{ssh.Password(cliConf.Password)}
	} else {
		config.Auth = []ssh.AuthMethod{publicKeyAuthFunc(cliConf.KeyFile)}
	}
	addr := fmt.Sprintf("%s:%d", cliConf.Host, cliConf.Port)

	if sshClient, err = ssh.Dial("tcp", addr, &config); err != nil {
		log.Println("[ERROR] error occurred:", err)
	}
	cliConf.sshClient = sshClient

	//此时获取了sshClient，下面使用sshClient构建sftpClient
	if sftpClient, err = sftp.NewClient(sshClient); err != nil {
		log.Println("[ERROR] error occurred:", err)
	}
	cliConf.sftpClient = sftpClient
}

// RunShell Run cmd
func (cliConf *SSH) RunShell(shell string) (res string, error1 error) {
	var (
		session *ssh.Session
		err     error
	)
	//获取session，这个session是用来远程执行操作的
	if session, err = cliConf.sshClient.NewSession(); err != nil {
		return "", err
	}
	//执行shell
	if output, err := session.CombinedOutput(shell); err != nil {
		return "", err
	} else {
		cliConf.LastResult = tools.Strip(string(output), "\n")
	}
	return cliConf.LastResult, nil
}

// Upload Upload file
func (cliConf *SSH) Upload(srcPath, dstPath string) error {
	srcFile, _ := os.Open(srcPath)                   //本地
	dstFile, _ := cliConf.sftpClient.Create(dstPath) //远程
	defer func() {
		_ = srcFile.Close()
		_ = dstFile.Close()
	}()
	buf := make([]byte, 1024)
	for {
		n, err := srcFile.Read(buf)
		if err != nil {
			if err != io.EOF {
				return err
			} else {
				break
			}
		}
		_, _ = dstFile.Write(buf[:n])
	}
	return nil
}

// UploadDirectory Upload Directory
func (cliConf *SSH) UploadDirectory(srcDir, dstPath string) error {
	srcFiles, err := ioutil.ReadDir(srcDir)
	if err != nil {
		return err
	}
	for _, backupDir := range srcFiles {
		srcFilePath := path.Join(srcDir, backupDir.Name())
		dstFilePath := path.Join(dstPath, backupDir.Name())
		if backupDir.IsDir() {
			cliConf.sftpClient.Mkdir(dstFilePath)
			cliConf.UploadDirectory(srcFilePath, dstFilePath)
		} else {
			cliConf.Upload(srcFilePath, dstFilePath)
		}
	}
	return nil
}

// Download file
func (cliConf *SSH) Download(srcPath, dstPath string) error {
	srcFile, _ := cliConf.sftpClient.Open(srcPath) //远程
	dstFile, _ := os.Create(dstPath)               //本地
	defer func() {
		_ = srcFile.Close()
		_ = dstFile.Close()
	}()

	if _, err := srcFile.WriteTo(dstFile); err != nil {
		return err
	}
	return nil
}

// DownloadDirectory Download Directory
func (cliConf *SSH) DownloadDirectory(srcPath, dstPath string) error {
	w := cliConf.sftpClient.Walk(srcPath)
	for w.Step() {
		if w.Err() != nil {
			continue
		}
		fileName := strings.Split(w.Path(), srcPath)
		stat, _ := cliConf.sftpClient.Stat(w.Path())
		if stat.IsDir() {
			err := os.MkdirAll(dstPath+fileName[len(fileName)-1], 0755)
			if err != nil {
				return err
			}
		} else {
			err := cliConf.Download(w.Path(), dstPath+fileName[len(fileName)-1])
			if err != nil {
				return err
			}
		}

	}
	return nil
}

// Delete delete remote file
func (cliConf *SSH) Delete(filePath string) error {
	return cliConf.sftpClient.Remove(filePath)
}
