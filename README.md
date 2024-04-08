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
./sub-server -dir /path/to/workDir -host 127.0.0.1:8080 -subcnv "http://127.0.0.1:25500"
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
│       ├── clash.ini
│       ├── ClashMeta.yaml
│       └── ClashMetaOnlyCN.yaml
└── sub-server
```

#### 文件重定向支持

重定向的判断是判断文件内容以 `[Redirect]` 开头，内容是 `ini` 格式。

```ini
[Redirect]
file=clash.ini # 必填
uuid=58cfbff0-18c8-1f7d-400a-ba07a305b1e6 # 选填，不填则沿用原 uuid
```

PS: 重定向视为可信输入，不再校验外部输入逻辑，此时 uuid 可为任意字符串。

#### subconverter 调用支持

subconverter 服务器默认地址 `http://127.0.0.1:25500`，可以通过启动参数 `-subcnv` 修改。

当前文件后缀为 `ini` 时，则认为是 subconverter 配置文件，将调用 subconverter 服务。文件内容写法与 [subconverter 配置档案](https://github.com/tindy2013/subconverter/blob/master/README-cn.md#%E9%85%8D%E7%BD%AE%E6%A1%A3%E6%A1%88)相同。调用时，是将配置档拆解成独立参数，然后调用的。

### systemd 服务

参考文件：[sub-server.service](https://github.com/alecthw/simple-sub-server/blob/master/sub-server.service)
