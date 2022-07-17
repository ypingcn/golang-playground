# Golang 游乐场 (Go Playground)

基于 [Golang 官方项目](https://github.com/golang/playground)调整而来，让你可以在本地快速启动一套 Golang Web Playground，来快速验证各种 Golang 代码片段。

![](./screenshot.png)

## 快速开始

想要运行程序，**首先**需要先安装 Docker，桌面操作系统可以访问官网[下载安装文件](https://www.docker.com/get-started/)，服务器版本的 Docker 安装，可以[参考这里](https://soulteary.com/2022/06/21/building-a-cost-effective-linux-learning-environment-on-a-laptop-the-basics.html#%E6%9B%B4%E7%AE%80%E5%8D%95%E7%9A%84-docker-%E5%AE%89%E8%A3%85)。

**然后**，执行下面的命令或者项目中的程序（`bash pull-images.sh`），获取必要的镜像文件：

```bash
docker pull soulteary/golang-playground:web-1.18.4
docker pull soulteary/golang-playground:sandbox-1.18.4
docker pull soulteary/golang-playground:actuator-1.18.4
docker pull memcached:1.6.15-alpine
```

**接着**，在镜像获取完毕之后，使用 `docker-compose up -d` 或 `docker compose up -d`，启动程序。

**最后**，打开浏览器，访问 `http://localhost:8080`，就可以开始 Golang 之旅啦。

