# Go Playground(Golang 游乐场)

基于 [Golang 官方项目](https://github.com/golang/playground)调整而来，让你可以在本地快速启动一套 Golang Web Playground，来快速验证各种 Golang 代码片段。

![](./screenshot.png)

## 项目特点

- 支持完全离线运行，无需“联网”，不需担心有任何信息泄漏的风险（比如包含密钥的程序）。
- 支持使用容器进行快速启动，不锁定任何公有云或者复杂的运行环境。
- 和官方程序一样，使用沙盒方式运行 Golang 程序，确保运行程序安全，无副作用。
- 和官方程序一样，使用 `faketime` “模块”，让程序能够提供确定性的输出，让程序复现和结果缓存变的更加容易。
- 合并了来自 `go.dev` 的默认示例，并进行了适当的界面“汉化”。
- 大幅精简程序模块和依赖，减少不必要的资源消耗。

## 快速开始

想要运行程序，**首先**需要先安装 Docker

**然后**，执行下面的命令或者项目中的程序（`bash make-images.sh`），构建必要的镜像文件：

```bash
docker pull ypingcn/golang-playground:web-1.23.4
docker pull ypingcn/golang-playground:sandbox-1.23.4
docker pull ypingcn/golang-playground:actuator-1.23.4
docker pull memcached:1.6.15-alpine
```

执行命令获取依赖的镜像文件（`bash make-images.sh`）

```bash
docker pull memcached:1.6.15-alpine
```

**接着**，在镜像获取完毕之后，如需使用私有仓库中的代码，请编辑 web 服务中`GOPRIVATE`、`GONOPROXY`和`GONOSUMDB`三个环境变量，将私有仓库地址添加到其中。

检查`docker_web_init_script.sh`脚本中的参数，申请账号后更新相关参数。

**最后**，使用 `docker-compose up -d` 或 `docker compose up -d`，启动程序。打开浏览器，访问 `http://localhost:8080`，就可以开始 Golang 之旅啦。


