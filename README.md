# sub-server

基于文件的简单订阅服务器。

主要为了方便在配置文件完全自己编写的情况下，快速导入Surge、QuantumultX、Loon等客户端。

## 编译

```bash
git clone https://github.com/alecthw/simple-sub-server.git
cd simple-sub-server
export GOOS=linux GOARCH=amd64 # 可选，交叉编译
go build -o sub-server

```

## 使用

配置文件存放：`{workdir}/sub/{uuid}/{file}`。

`uuid`必须是合法的uuid，做了校验。

代码中做了防越级处理，不能添加父子路径，即 `file` 的值中不能包含字符 `/` 、 `\` 和 `..` 。

请求url：`http://127.0.0.1:8080/{uuid}/{file}`。

### 运行

```bash
./sub-server -dir /path/to/workDir -host 127.0.0.1:8080
```

### 工作目录示例

```txt
{workdir}
├── sub
│   ├── 56d00b21-554d-5a90-6daa-52537050fb20
│   │   ├── Loon.conf
│   │   ├── QuantumultX.conf
│   │   ├── Stash.yaml
│   │   └── Surge.conf
│   └── 58cfbff0-18c8-1f7d-400a-ba07a305b1e6
│       ├── ClashMeta.yaml
│       └── ClashMetaOnlyCN.yaml
└── sub-server
```

### systemd 服务

参考文件：[sub-server.service](https://github.com/alecthw/simple-sub-server/blob/master/sub-server.service)
