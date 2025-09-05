# Harmony设备投屏演示

这是一个简化的Harmony设备投屏演示程序，使用Go语言实现后端，HTML实现前端界面。

## 文件结构

```
demo/
├── demo.go          # Go后端程序
├── index.html       # HTML前端页面
├── go.mod          # Go模块文件
├── start.sh        # Linux/macOS启动脚本
├── start.bat       # Windows启动脚本
└── README.md       # 说明文档
```

## 功能特性

- 🔗 **设备扫描**: 自动扫描Harmony设备（支持真实设备和模拟设备）
- 📱 **模拟投屏**: 模拟设备屏幕投屏功能
- 🖱️ **交互界面**: 现代化的Web界面
- 📡 **REST API**: 提供设备管理API接口

## 快速开始

### 方法1: 使用启动脚本

**Linux/macOS:**
```bash
chmod +x start.sh
./start.sh
```

**Windows:**
```cmd
start.bat
```

### 方法2: 手动启动

```bash
# 确保在demo目录下
cd go_project/demo

# 运行程序
go run demo.go
```

### 访问应用

打开浏览器访问: http://localhost:8080

## 使用说明

1. **启动程序**: 运行启动脚本或手动执行`go run demo.go`
2. **访问界面**: 在浏览器中打开 http://localhost:8080
3. **选择设备**: 在左侧设备列表中选择要投屏的设备
4. **开始投屏**: 点击"开始投屏"按钮
5. **查看效果**: 在右侧屏幕区域查看模拟的投屏效果

## API接口

### 获取设备列表
```http
GET /api/devices
```

### 启动投屏
```http
POST /api/devices/{deviceKey}/start
```

### 停止投屏
```http
POST /api/devices/{deviceKey}/stop
```

## 技术说明

### 后端 (Go)
- 使用标准库`net/http`提供HTTP服务
- 支持设备扫描和管理
- 提供REST API接口
- 支持模拟设备功能

### 前端 (HTML + JavaScript)
- 纯HTML + CSS + JavaScript实现
- 响应式设计，支持不同屏幕尺寸
- 实时设备状态显示
- 模拟投屏界面

### 设备支持
- **真实设备**: 如果系统中有HDC工具和连接的Harmony设备
- **模拟设备**: 如果没有真实设备，自动创建模拟设备进行演示

## 注意事项

1. 确保Go 1.21或更高版本已安装
2. 如果使用真实设备，需要安装HDC工具
3. 程序默认运行在8080端口
4. 支持跨平台运行（Windows、macOS、Linux）

## 故障排除

### 常见问题

**1. 无法启动程序**
- 检查Go是否正确安装
- 确认在正确的目录下运行
- 检查端口8080是否被占用

**2. 无法访问网页**
- 确认程序已成功启动
- 检查防火墙设置
- 尝试使用 http://127.0.0.1:8080

**3. 设备列表为空**
- 检查HDC工具是否安装
- 确认Harmony设备已连接
- 程序会自动创建模拟设备

## 扩展开发

这个演示程序可以作为基础，进一步开发完整的功能：

1. **添加WebSocket支持**: 实现实时图像传输
2. **集成真实HDC**: 连接真实的Harmony设备
3. **图像处理**: 添加图像压缩和优化
4. **交互功能**: 实现真实的触摸和按键事件
5. **UI优化**: 改进界面设计和用户体验

## 许可证

本项目采用 MIT 许可证。