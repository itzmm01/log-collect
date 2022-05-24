## 参数



```bash
Usage of log-collect:
# 日志保存目录,默认"/tmp/logs"
  -d string
        dest logs dir (default "/tmp/logs")
# 打印更详细的日志,默认不打印
  -debug
        debug
# 指定host.yml文件 拉起主机日志时使用,默认"./host.yml"
  -i string 
        host.yml (default "./host.yml")
# io限制最大多少 MB
  -limit int 默认0, 0表示不限制
        Limit Max Speed: 1MB/s (0=unlimited)
# 模式： list-列出支持的日志名称 get-拉起日志    (必要参数)
  -m string
        mode: list/get
# 指定拉起日志名,配合 -m get 使用
  -n string
        log name
```

## 配置文件

`host.yml`

```yaml
test:
  - ip: x.x.x.x
    user: root
    port: 22
    password: xxx
  - ip: x.x.x.x
    user: root
    port: 22
    password: xxx
```

`conf.yml`

```yaml
logs:
# 主机日志
  - type: ssh
# 日志名
    name: test
# 日志存放目录
    dir: /root
# 日志文件名，为空的话拉取整个目录
    file: "naviacat*.zip"
# 指定主机组
    hostgroup: test
    
# pod日志
  - type: k8s
# 日志名
    name: test2
# 命名空间
    namespace: default
# pod名使用关键字即可, 例如: hello-world-3c82s hello-world-z5fgs 填写hello-world即可
    pod: hello-world

# 日志存放目录
    dir: /var/log
# 日志文件名，为空的话拉取整个目录
    file: "yum*"
```



## 示例

```bash
# 列出所有日志
./log-collect -m list
# 拉取 test日志
./log-collect -m get -n test
# 拉取 test日志,限制io为 5MB/s, 执行完成没有报错会输出压缩后的日志路径，将其下载提供即可
# 2022/05/09 18:26:15 main.go:175: INFO logfile path: /tmp/logs/wemeet-center.tar.gz
./log-collect -m get -n test -limit 5
```



