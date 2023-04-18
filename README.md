# sub-server

简单订阅服务器。

主要为了方便在配置文件完全自己编写的情况下，快速导入Surge、QuantumultX、Loon等客户端。

## 使用

使用`id <--> filePath`的简单对应方式，配置文件放在程序运行目录下的`sub`文件夹下。

### 配置文件

配置文件放在工作目录下，默认是`cfg.json`，可通过参数指定。

```json
{
  "address": "127.0.0.1",
  "port": 8080,
  "files": {
    "324e61ce-1d09-a93e-23a0-2205f3b86661": "ClashMeta.yaml",
    "08a5e389-2538-d9b2-e740-d2550b977317": "ClashMetaOnlyCN.yaml"
  }
}
```

### 运行

```bash
./sub-server --dir=/path/to/workDir --cfg=cfg.json
```
