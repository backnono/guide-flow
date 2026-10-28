# Guide Flow 前端

Guide Flow 前端是一个基于 Vue 3 的互动式数学题讲解页面，负责承载从图片上传、题目确认到解题过程展示和练习反馈的用户流程。

## 技术栈

- Vue 3
- TypeScript
- Vite
- Pinia
- Sass

## 本地启动

在项目根目录执行：

```bash
cd frontend
npm install
npm run dev
```

启动后访问：

```text
http://localhost:5173
```

开发服务器会将 `/api` 请求代理到后端：

```text
http://127.0.0.1:8080
```

因此使用完整流程时，需要同时启动后端服务。

## 常用命令

```bash
# 启动开发服务器，支持热更新
npm run dev

# 类型检查并构建生产版本
npm run build

# 预览已经构建完成的生产版本
npm run preview
```

## API 地址配置

默认情况下，前端通过相对路径 `/api` 访问后端，并使用 Vite 代理转发到 `127.0.0.1:8080`。

如果需要让前端直接访问其他后端地址，可以配置 `VITE_API_BASE_URL`：

```bash
VITE_API_BASE_URL=http://127.0.0.1:8080/api npm run dev
```

前端不保存也不读取 DeepSeek API Key。LLM 密钥只应配置在后端启动命令或后端运行环境中。

## 目录概览

```text
src/
├── App.vue             # 页面入口和主要交互流程
├── app.scss            # 全局页面样式
├── main.ts             # Vue 应用初始化
├── services/           # 后端 API 请求封装
├── stores/              # Pinia 状态管理
└── types/              # 前端领域类型定义
```

## 当前流程

前端当前围绕以下状态展示后端处理进度：

1. 上传题目图片
2. 查看 OCR 识别结果并确认题目
3. 展示题目结构化信息和解题过程
4. 展示讲解步骤、易错点和练习题
5. 根据后端返回的状态和错误信息提示用户下一步操作

后端仍在逐步接入真实 OCR、LLM 和检索服务；前端通过 API 契约与这些外部能力解耦。
