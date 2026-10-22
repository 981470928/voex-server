# Docker Hub 完整安装镜像

仓库：[chenrui9814/voex](https://hub.docker.com/r/chenrui9814/voex)。当前唯一安装标签为 **`install-0.2.0`**，平台 **Linux amd64**。

一个总镜像包含前端 dist、API、updater、官方 Nginx 与 MySQL 8.0 的完整 Docker 镜像归档、Compose 配置、安装脚本、签名清单和发布公钥。只需拉取这一个镜像，提取后即可离线安装，无需再拉取运行组件。执行一次 `docker run` 即可自动安装；安装容器退出后，业务容器继续运行。

安装后 Nginx、MySQL、API、updater 各自运行在独立容器中。后续应用升级仍使用带签名的 v2 离线升级包，只包含 API、前端和数据库迁移，不重复携带 Nginx、MySQL。

## 安装条件

Linux x86_64 主机、rootful Docker Engine，以及运行 Docker 的权限。安装镜像已包含 Bash、Python 3、Docker CLI 和 Compose v2，宿主机无需另装这些工具。初次拉取总镜像需要网络，提取后的安装不依赖镜像仓库。请预留足够磁盘空间用于总镜像、提取文件、导入的运行镜像和业务数据；建议至少 5 GiB 可用空间，数据增长另计。

首次安装前，通过可信渠道核对安装镜像 digest 或发布公钥 SHA-256 指纹：

```text
f09900e3065a163278dafebb066c8f020afcbf102bb590ad7e9c69ae61ee9e71
```

同包公钥验签只证明文件与该公钥一致，不能独立证明发布者身份。后续升级使用首次安装保存的发布公钥。

## 一条命令安装

在 Linux amd64 Docker 主机上执行这一条命令：

```bash
sudo docker run --rm --name voex-installer --pull=always --platform linux/amd64 --network host -v /var/run/docker.sock:/var/run/docker.sock -v /opt/voex:/opt/voex -v /home/static:/home/static chenrui9814/voex:install-0.2.0
```

安装成功后访问 `http://服务器地址:8080/app/`。安装容器自动删除，MySQL、Nginx、API、updater 继续独立运行。

- `--pull=always` 获取支持自动安装的新镜像，避免误用本地缓存的旧文件载体版本；已预先拉取并需要离线安装时，可改为 `--pull=never`。
- `--name voex-installer` 供安装器检查自身的挂载与网络配置；同一时间只运行一个安装器。
- Docker socket 用于在宿主机导入镜像、创建业务容器，其权限等同于管理宿主机 Docker；仅对可信发布镜像使用。
- `/opt/voex` 和 `/home/static` 均按相同绝对路径绑定，分别保存部署数据与附件，安装容器退出后仍保留。默认绑定会自动创建缺失的目录。
- `--network host` 让安装器可以检查宿主机上业务服务的就绪状态；业务服务的访问地址由安装参数控制。
- 不需要 `-p`、`--privileged` 或挂载宿主机根目录。

镜像名后可依次传入部署目录、HTTP 端口、IPv4 绑定地址，默认分别为 `/opt/voex`、`8080`、`0.0.0.0`。例如改用 18080 端口并仅监听本机：

```bash
sudo docker run --rm --name voex-installer --pull=always --platform linux/amd64 --network host -v /var/run/docker.sock:/var/run/docker.sock -v /opt/voex:/opt/voex -v /home/static:/home/static chenrui9814/voex:install-0.2.0 /opt/voex 18080 127.0.0.1
```

自定义部署目录时，须同时修改绑定的宿主机路径、容器路径以及第一个安装参数，三者保持完全相同。部署目录必须不存在或为空，不得与 `/home/static` 重叠。已有 runtime.env、相同 Compose 项目的容器或数据卷时会拒绝覆盖。

需要自定义项目名或反向代理 Origin，可在镜像名之前添加 `-e VOEX_PROJECT_NAME=项目名`、`-e VOEX_ALLOWED_ORIGINS=https://docs.example.com`。项目名默认 `voex`；Origin 为逗号分隔的 HTTP(S) Origin，不带路径或尾部 `/`，同源请求始终允许。

仅查看帮助，不会安装或访问宿主机 Docker：

```bash
docker run --rm --pull=always --platform linux/amd64 chenrui9814/voex:install-0.2.0 --help
```

若仍需手动提取安装文件，可使用 `docker create`、`docker cp 容器ID:/release/. 目标目录/`，然后在宿主机上执行 `sudo ./install-hub.sh`；这种方式仍要求宿主机具备 Bash、Python 3 和 Docker Compose v2。

## 安装内容与数据

安装器先验签，再核对 images.tar、frontend.tar.gz、Compose、Nginx 配置和安装脚本的 SHA-256，导入四个运行镜像并核对 ID 与平台，之后才创建部署配置和启动服务。

MySQL 密码、JWT 密钥与 updater token 在目标主机随机生成，敏感文件权限为 `0600`，不随公共镜像交付。启动数据库和 updater 后，显式运行 API migrate，再启动 API 和 Nginx；只有 API、前端版本及数据库 schema 均匹配，才解除维护模式。失败时保留维护标记和配置用于恢复，不覆盖已有部署。

| 位置 | 内容 |
| --- | --- |
| `/opt/voex/runtime.env` | 部署参数、随机数据库密码与 updater token |
| `/opt/voex/secrets/jwt-key` | 随机 JWT 密钥 |
| `/opt/voex/frontend/releases/` | 前端版本目录 |
| `/opt/voex/frontend/current` | 当前前端版本链接 |
| `/opt/voex/frontend/assets/` | 保留旧页面需要的 hash 资源 |
| `/opt/voex/backups/` | 升级前备份 |
| `/home/static` | 附件和资产；不迁移历史 R2 文件 |
| `voex_mysql-data` | 默认 Compose 项目的 MySQL 命名卷 |

公开前端不嵌入个人 Pexels API Key。需要 Pexels 图片搜索时，部署方须配置自己的 `VITE_PEXELS_API_KEY` 后重新构建前端；当前没有运行时配置该 Key 的界面。

所有已登录用户均可使用系统设置里的离线升级入口。

查看服务状态：

```bash
sudo docker ps --filter label=com.docker.compose.project=voex
```

## 构建与发布维护

在构建主机先准备 Linux amd64 的 `nginx:latest` 和 `mysql:8.0` 基础镜像；也可以通过可信离线归档导入。构建脚本不会自动访问 Docker Hub 拉取缺失镜像。然后从当前前后端源码执行完整构建和镜像导出：

```bash
cd /home/voex-server
./deploy/build-install-image.sh 0.2.0 chenrui9814/voex
```

输出 `chenrui9814/voex:install-0.2.0` 以及 `/home/voex-server/dist/voex-install-0.2.0.tar`，不执行安装或推送。编译默认读取前端目录 `/home/voex`，可用 `VOEX_FRONTEND_DIR` 指定其他路径。Nginx 基础层通过已检查的本地 image ID 固定，不依赖旧版本的远程 digest，也支持导入后没有 RepoDigests 的镜像。

仅重新组装已有 API/updater 构建产物时，才单独运行 `./deploy/prepare-hub-release.sh 0.2.0 chenrui9814/voex`。缺少镜像与平台不匹配会分别报错。

该脚本独立构建公共前端，显式清空 Pexels Key，检查 dotenv 与环境中的敏感 VITE 变量值是否出现在产物中，白名单打包十个文件、生成签名，并加入带依赖工具的安装入口后构建 `install-0.2.0`。不修改私有 .env，不自动推送、不执行安装。请始终保留既有签名私钥，仅在可信构建主机使用，不放入镜像。

发布只推送一个总镜像标签：

```bash
docker push chenrui9814/voex:install-0.2.0
```

如果使用 crane 代替 Docker push，应把 Docker save 导出的 OCI archive 安全解压为 OCI layout 目录，再以该目录执行 `crane push`，避免从 legacy manifest 转换导致外层镜像 digest 改变。包内四个运行镜像直接保留原始 Docker save 归档，不转换格式。

## 许可证

Voex 使用 **PolyForm Noncommercial License 1.0.0**；完整许可证随包提供。镜像公开不代表额外商业授权。包内 Nginx、MySQL 等第三方组件保留各自许可证与使用条款。
