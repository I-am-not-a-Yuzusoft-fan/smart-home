# 队友上岗说明（Windows）

这份文档写给要接手 **#9 类型模板支线** 的队友。看完你应该能：把项目在自己电脑上跑起来、
知道代码在哪、知道哪些文件不能碰、知道怎么把改动合进 `main`。

仓库：<https://github.com/I-am-not-a-Yuzusoft-fan/smart-home>

---

## 1. 先把环境装好（约 30 分钟）

| 装什么 | 用来干什么 | 注意 |
|---|---|---|
| **Git for Windows** | 拉代码、提交、推送 | 安装时保持默认，它会顺带装 **Git Bash**，后面所有命令都在 Git Bash 里敲 |
| **Node.js 20 LTS 或更新** | 前端构建 | 装完 `node -v` 能出版本号 |
| **Go 1.27 或更新** | 后端编译 | `go version` 应显示 `go1.27` 或更高 |
| **Docker Desktop** | 跑 PostgreSQL，不用自己装数据库 | 设置里勾 **Use the WSL 2 based engine**，装完先启动它，托盘图标变绿再用 |

**不需要装 PostgreSQL 或 psql。** 数据库跑在容器里，仓库脚本会自动用容器内的 `psql`。

装完在 Git Bash 里验一遍：

```bash
git --version && node -v && go version && docker version --format '{{.Server.Version}}'
```

最后一行能打印出版本号，说明 Docker 已经在跑。

## 2. 把项目跑起来

```bash
git clone https://github.com/I-am-not-a-Yuzusoft-fan/smart-home.git
cd smart-home
bash start.sh
```

第一次会慢一些（拉镜像、`npm ci` 装前端依赖）。看到这三行就成功了：

```
前端： http://localhost:5173
接口： http://localhost:8081/api/healthz
演示账号：zhang / ...（管理员）、li / ...（家庭成员）
```

用浏览器打开 <http://localhost:5173>，用上面的演示账号登录。口令写在 `sql/seed.sql`
的注释里 —— 那是**演示口令，不是密钥**，别拿到别处复用。

按 `Ctrl-C` 停止。数据库容器会保留，下次启动更快；想彻底清空用
`docker compose down -v`（注意：数据没了，下次启动会重新灌 seed）。

### 常见问题

| 症状 | 原因与处理 |
|---|---|
| `bash: $'\r': command not found` | 文件被按 CRLF 检出。`git config core.autocrlf` 必须是空或 `false`，然后 `git rm --cached -r . && git reset --hard` 重新落盘 |
| `docker compose` 报 `project name must not be empty` | 目录名里有中文/特殊字符。仓库已写死 `name: smarthome`，若还报，确认 `docker-compose.yml` 第一行是 `name: smarthome` |
| 5433 端口被占 | 本机已有 PostgreSQL 在跑。改 `docker-compose.yml` 的端口映射，同时改 `DATABASE_URL` |
| 前端起不来但后端正常 | 先在 `frontend/` 下 `npm ci`；`start.sh` 只在 `node_modules` 不存在时才自动装 |
| 页面能开但接口 401 | 正常，说明没登录。先登录，Cookie 是 `httpOnly` 的，别指望在 localStorage 里找 |

## 3. 仓库长什么样

```
backend/          Go 服务（五层：api / policy / control / device / store）
  api/            接口层：handler、鉴权、路由
  api/gen/        由 openapi.yaml 生成的代码，不要手改
  store/          数据访问，sqlc 生成的代码在 store/gen/
  cmd/api/        入口 main.go
frontend/         React + TypeScript + Vite + Konva（平面图）
openapi.yaml      接口契约，**唯一的接口事实来源**
sql/              建表、seed、查询
docs/             文档（需求、设计、ADR、图表）
scripts/          reset.sh（重建演示数据）、build-diagrams.sh（重画图）
start.sh          一键启动
```

跑测试：

```bash
cd backend
export GOPATH=/tmp/gopath GOMODCACHE=/tmp/gopath/pkg/mod GOCACHE=/tmp/gocache
docker compose -f ../docker-compose.yml up -d db
TEST_DATABASE_URL='postgres://postgres:spike@127.0.0.1:5433/smarthome?sslmode=disable' go test ./...
```

（Windows 上 `GOPATH` 那一行可以换成你自己的路径；不设也能跑，只是缓存落在用户目录。）

## 4. 你的任务：#9 类型模板

### 为什么要有它

需求里有一条硬指标 **FR-2.4：新增类型不得修改前端代码** —— 管理员在界面上新建一个类型、
配好能力项，这个类型立刻能出现在家具库里布放，属性表单按能力项**自动生成**。
所以「类型」必须是**数据**，不能是代码里的枚举。

### 地基已经打好了，你在这上面盖

- 表 `item_type` 已存在（`sql/0001_init.sql`），字段：
  `key`（主键，如 `ceiling_light`）、`kind`（`furniture` / `device`）、`name`、`icons`、
  `default_width`、`default_depth`、`capabilities`（jsonb 数组）、`capabilities_version`、`driver`。
- `sql/seed.sql` 里已经有 11 个类型模板可直接对照着看，比如 `ceiling_light` 的
  `capabilities` 长这样（**这就是你要驱动的那个结构**）：

  ```json
  [{"key":"on","name":"开关","type":"bool","writable":true,"history":true},
   {"key":"brightness","name":"亮度","type":"number","unit":"%","min":0,"max":100,"step":1,"writable":true,"history":true},
   {"key":"color_temp","name":"色温","type":"number","unit":"K","min":2700,"max":6500,"step":100,"writable":true,"history":true}]
  ```

  完整字段约定见 `docs/adr/0002-数据模型与分层.md` §2.3：
  `{key, name, type(bool|number|enum|string), unit, min, max, step, options, writable, default, history, icons}`。
- 契约先行：接口先写进 `openapi.yaml`，再跑代码生成，最后填 handler。

### 你要交付的

1. **接口**：类型模板的增删改查（`/api/item-types`），以及列表给前端用。
   写进 `openapi.yaml` 后执行（在 `backend/` 下）：

   ```bash
   go run github.com/oapi-codegen/oapi-codegen/v2/cmd/oapi-codegen@v2.8.2 \
     --config=api/oapi-codegen.yaml ../openapi.yaml
   ```

   然后在 `backend/api/` 里实现新生成的方法。**不要手改 `api/gen/` 下的文件。**
2. **前端**：一个「类型管理」页面（管理员可见）+ 动态表单组件。
   表单按 `type` 分四支渲染：`bool` → 开关、`number` → 数字输入（带 min/max/step）、
   `enum` → 下拉（选项来自 `options`）、`string` → 文本输入。
   家具库那一栏要改成从接口读列表，不再写死。
3. **测试**：至少覆盖三条 ——
   - 建一个类型 → 出现在列表里；
   - 建一个类型 → 立刻能布放并操作（这条就是 FR-2.4，验收会当场做）；
   - 越界值（比如亮度 120）在下发命令时被拒绝。

### 不要做的事

- **不要碰 `#5` 的文件**：画墙、房间推导、`Canvas2D` 组件、`corner`/`wall`/`opening` 相关代码。
- **不要建新表**：`item_type` 已经够用；真要加字段，先跟组长说，走迁移。
- **不要提交** `node_modules/`、`.cache/`、`*.exe`、任何口令或 token。
- 不要图省事直接把类型写成前端常量 —— 那样正好违背了这条需求的验收点。

## 5. 怎么把改动合进去

`main` 受保护：**不能直接 push，必须走 PR，且必须有 1 个 Approve（组长）**，CI 全绿才能合。

```bash
git checkout main && git pull
git checkout -b feature/t6-item-types
# ……写代码……
git add <具体文件>          # 不要用 git add -A，容易把无关文件带进来
git commit -m "feat(types): 类型模板增删改查与动态表单"
git push -u origin feature/t6-item-types
```

然后在 GitHub 上开 PR，正文写三件事：**做了什么、为什么、怎么验**，
并在里面写 `Closes #9`。等 CI 两项变绿、组长点了 Approve，就可以 squash 合并。

冲突预警：`#5`（画墙）也会改 `openapi.yaml` 和 `sql/seed.sql`。约定是 **#5 先合，你 rebase 后再合**；
真冲突了以 `main` 为准，把你这边的接口重新加回 `openapi.yaml` 再生成一次。

## 5.1 用 curl 调接口时

写请求（POST / PUT / PATCH / DELETE）必须带 `X-Requested-With: fetch` 头，否则后端返回 403。这是 CSRF 防护：跨站表单带不了自定义头，同源页面能带。漏了这个头，浏览器里的页面一切正常，只有手写脚本会撞上。

```bash
curl -X POST http://127.0.0.1:8081/api/auth/login \
  -H 'Content-Type: application/json' \
  -H 'X-Requested-With: fetch' \
  -c /tmp/c.txt -d '{"username":"zhang","password":"demo1234"}'
curl -b /tmp/c.txt http://127.0.0.1:8081/api/plan
```

## 6. 文档在哪

| 想知道什么 | 看哪 |
|---|---|
| 名字都什么意思（capability / item_type / state…） | `CONTEXT.md` |
| 需求原文与验收方法 | `docs/deliverables/02-需求规格说明书.md`（F2 是 FR-2.x） |
| 表结构与字段约定 | `docs/adr/0002-数据模型与分层.md` |
| 接口与错误码 | `docs/deliverables/04-详细设计说明书.md` §3、§5 |
| 为什么这么选（技术栈、架构） | `docs/adr/` |
| 协作与 Git 规则 | `docs/agents/git-workflow.md` |

## 7. 卡住了

- 先看 `git log --oneline -10` 和对应 issue 的评论，多数问题前面都遇到过。
- 跑不起来就把**完整报错**贴到 issue 里，别只说「跑不起来」。
- 需要改契约或表结构这类影响别人的事，先在 issue 里说一声，不要自己先合。
