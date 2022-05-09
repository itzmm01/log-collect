## 日志拉取

### 查看支持的日志模块



```bash
# 列出所有支持的日志模块
[root@tcs-172-16-16-50 log-collect-linux-amd64]# ./log-collect -m list
wemeet-center
wemeet-conn
wemeet-xmpp
cmlb_agent
cmlb_cc
logic

Usage: ./log-collect -m get -n xxx

```

### 拉取日志

```bash
# -m get -n 模块名
[root@tcs-172-16-16-50 log-collect-linux-amd64]# ./log-collect -m get -n wemeet-center
2022/05/09 18:26:15 main.go:175: INFO logfile path: /tmp/logs/wemeet-center.tar.gz
```

*执行完成没有报错会输出压缩后的日志路径，将其下载提供即可*