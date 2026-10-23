# GitHub 与云服务器部署关联

源码仓库：https://github.com/haggaimurr-dot/bailiankai-medical

本地开发目录和云服务器源码目录均使用上述仓库作为 `origin`，跟踪 `codex-dev` 分支。后续服务器开发提交统一提交到 `codex-dev`。

## 云服务器目录

| 目录 | 用途 |
| --- | --- |
| `/opt/bailiankai/repository` | Git 源码工作副本，跟踪 `origin/codex-dev` |
| `/opt/bailiankai/releases/20261023-01` | 首次部署的可执行文件、后台静态文件及部署脚本 |
| `/opt/bailiankai/current` | 当前运行版本的符号链接 |
| `/etc/bailiankai` | 服务器私有环境配置和数据库凭据，不进入 Git |
| `/var/lib/bailiankai` | 数据库、私有图片及服务状态，不进入 Git |
| `/var/backups/bailiankai` | 服务器本机备份，不进入 Git |

首次上传包部署完成后补建 Git 仓库，业务源码基线为 `738c491c294277b827d80c0c35d973a8910d2040`。运行目录内的 `SOURCE_REPOSITORY.json` 记录来源及部署文件校验值；这不是从 Git 在服务器重新编译的记录。

## 拉取后续源码

通过已有服务器管理通道登录后执行：

```bash
git -C /opt/bailiankai/repository status --short --branch
git -C /opt/bailiankai/repository pull --ff-only origin codex-dev
git -C /opt/bailiankai/repository rev-parse HEAD
```

公开仓库通过 HTTPS 只读拉取，不在服务器存放 GitHub 写入令牌或部署密钥。服务器工作副本的默认推送目标为 `origin/codex-dev`；直接推送仍需可用的 GitHub 写入认证。

拉取只更新源码。发布仍需构建、上传到新的 release 目录、执行迁移及检查后切换 `current`，不能把源码拉取当作部署成功。当前没有配置 GitHub 推送自动部署，GitHub Actions 的 checks 工作流仅执行代码检查与测试。

服务器现有 `bailiankai-api`、`bailiankai-web`、`bailiankai-mysql` 服务继续使用 `current` 目录中的发布包。数据库、图片和环境配置独立保留，不随 Git 拉取覆盖。
