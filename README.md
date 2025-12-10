# Antigravity2API - Go 版本

将 Google Gemini Code Assist 内部 API 转换为 OpenAI 兼容格式的代理服务。

## 功能特性

- **协议转换**: OpenAI 兼容 API (`/v1/chat/completions`)
- **Gemini API 兼容**: 标准 Gemini API 格式 (`/v1beta/*`)
- **OAuth 认证**: Google OAuth 2.0 授权流程，Token 自动刷新
- **多端点轮询**: 支持 daily/autopush/production 三端点负载均衡
- **端点热切换**: 运行时切换端点模式，无需重启
- **管理面板**: Web UI 管理凭证、查看配置
- **多模型支持**: Gemini 3 Pro 系列、Claude 系列

## 快速开始

### Windows / Termux

1. 从 [Releases](../../releases) 下载对应平台的二进制文件

2. 创建 `.env` 文件：
```bash
PANEL_USER=admin
PANEL_PASSWORD=your-password
API_KEY=sk-your-api-key
```

3. 运行：
```bash
# Windows
anti2api-windows-amd64.exe

# Linux/Termux
chmod +x anti2api-linux-arm64
./anti2api-linux-arm64
```

4. 访问 `http://localhost:8045/admin` 进入管理面板

### Docker

```bash
# 使用 docker-compose
docker-compose up -d

# 或直接运行
docker run -d \
  -p 8045:8045 \
  -v ./data:/app/data \
  -e PANEL_USER=admin \
  -e PANEL_PASSWORD=your-password \
  -e API_KEY=sk-your-api-key \
  ghcr.io/your-username/anti2api-golang:latest
```

## 环境变量

| 变量 | 说明 | 默认值 |
|------|------|--------|
| `PORT` | 服务端口 | 8045 |
| `HOST` | 监听地址 | 0.0.0.0 |
| `PANEL_USER` | 管理面板用户名 | admin |
| `PANEL_PASSWORD` | 管理面板密码 | **必填** |
| `API_KEY` | API 访问密钥 | 空（不验证） |
| `ENDPOINT_MODE` | 端点模式 | daily |
| `DEBUG` | 日志级别 (off/low/high) | off |
| `TIMEOUT` | 请求超时（毫秒） | 180000 |
| `PROXY` | HTTP 代理 | 空 |

### 端点模式

- `daily`: 仅使用 Daily 端点
- `autopush`: 仅使用 Autopush 端点
- `production`: 仅使用 Production 端点
- `round-robin`: 三个端点轮询
- `round-robin-dp`: Daily 和 Production 轮询

**支持热切换**：在管理面板"系统设置"中可直接切换，无需重启。

## 管理面板

访问 `http://localhost:8045/admin` 登录后可使用：

### 授权
1. 点击"获取授权 URL"打开 Google 授权页面
2. 完成 Google 登录后，会显示回调 URL
3. 点击"复制 URL"按钮复制
4. 返回管理面板，粘贴到输入框并提交

### 导入
- 支持 TOML 格式批量导入凭证
- 可选覆盖现有账号

### 管理凭证
- 查看凭证状态
- 刷新/启用/停用/删除凭证

### 系统设置
- 查看配置
- 热切换端点模式

## API 端点

### OpenAI 兼容

```bash
# 模型列表
GET /v1/models

# 聊天完成
POST /v1/chat/completions
Authorization: Bearer sk-your-api-key

# 指定凭证调用
POST /{credential}/v1/chat/completions
```

### Gemini 兼容

```bash
# 模型列表
GET /v1beta/models

# 非流式生成
POST /v1beta/models/{model}:generateContent?key=sk-your-api-key

# 流式生成
POST /v1beta/models/{model}:streamGenerateContent?key=sk-your-api-key
```

### 原始 Gemini 透传

```bash
POST /gemini/v1beta/models/{model}:generateContent
POST /gemini/v1beta/models/{model}:streamGenerateContent
```

## 支持的模型

- `gemini-3-pro-high` - Gemini 3 Pro (高质量)
- `gemini-3-pro-low` - Gemini 3 Pro (快速)
- `gemini-3-pro-image` - Gemini 3 Pro (图片生成)
- `gemini-3-pro-high-bypass` - Bypass 模式
- `claude-opus-4-5-thinking` - Claude Opus 4.5 (思考模式)
- `claude-sonnet-4-5` - Claude Sonnet 4.5
- `claude-sonnet-4-5-thinking` - Claude Sonnet 4.5 (思考模式)

## 本地开发

```bash
# 安装依赖
go mod download

# 运行
go run ./cmd/server

# 带调试日志
go run ./cmd/server -debug high

# 构建
go build -o anti2api ./cmd/server
```

## 项目结构

```
anti2api-golang/
├── cmd/server/          # 入口
├── internal/
│   ├── api/             # API 客户端
│   ├── auth/            # OAuth 认证
│   ├── config/          # 配置管理
│   ├── converter/       # 协议转换
│   ├── logger/          # 日志
│   ├── server/handlers/ # HTTP 处理器
│   ├── store/           # 数据存储
│   └── utils/           # 工具函数
├── public/admin/        # 前端文件
└── data/                # 数据目录
```

## 数据持久化

- 凭证: `./data/accounts.json`
- 设置: `./data/settings.json`

## 许可证

MIT License
