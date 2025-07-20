# Cursor & Windsurf Reset Tool

An advanced tool for resetting Cursor and Windsurf application data, supporting the latest versions of Cursor and Windsurf.
[中文说明](https://github.com/whispin/Cursor_Windsurf_Reset/blob/main/README_ZH.md)
## ✨ Interface Screenshot

![Interface Screenshot](https://github.com/whispin/Cursor_Windsurf_Reset/blob/main/screenshot/homepage.jpg?raw=true)

## ✨ Features

### 🎯 Core Features
- **Latest Version Support**: Cursor 1.2.1, Windsurf 1.10.7
- **Smart Reset**: Automatically detects and resets device IDs, session data, and cache for Cursor and Windsurf
- **Dual Interface Support**: Provides modern graphical interface and full-featured command-line interface
- **Cross-Platform Compatibility**: Supports Windows, macOS, and Linux systems
- **Safe Backup**: Automatically creates data backups before reset, supports one-click recovery

## 📦 Installation Instructions

### Method 1: Download Pre-compiled Version
1. Visit the [Releases page](https://github.com/whispin/Cursor_Windsurf_Reset/releases)
2. Download the version suitable for your system:
   - Windows: `Cursor_Windsurf_Reset-windows.exe`
   - macOS: `Cursor_Windsurf_Reset-macos`
   - Linux: `Cursor_Windsurf_Reset-linux`
3. Double-click to run (Windows) or execute in terminal

### Method 2: Build from Source (Ubuntu 22.04)

#### Prerequisites
- Go 1.21 or higher
- Build tools and OpenGL libraries

#### Quick Setup for Ubuntu 22.04

**如果遇到编译错误 (推荐先试这个)**:
```bash
# 快速修复 OpenGL/X11 依赖问题
chmod +x fix_ubuntu_deps.sh
./fix_ubuntu_deps.sh
```

**完整构建流程**:
```bash
# Method 1: One-click build
chmod +x build_ubuntu.sh
./build_ubuntu.sh

# Method 2: Manual step-by-step
chmod +x install_deps_ubuntu.sh
./install_deps_ubuntu.sh
make all
```

#### Manual Setup
```bash
# Install system dependencies
sudo apt update
sudo apt install -y build-essential libgl1-mesa-dev libglu1-mesa-dev \
    mesa-common-dev libglfw3-dev libx11-dev libxcursor-dev libxrandr-dev \
    libxinerama-dev libxi-dev libxxf86vm-dev libxext-dev libxfixes-dev \
    libxrender-dev libxss-dev pkg-config libgtk-3-dev \
    libayatana-appindicator3-dev libxapp-dev

# Install Go dependencies and build
go mod tidy
go build -o Cursor_Windsurf_Reset .
```

#### Troubleshooting Ubuntu Build Issues

**常见错误及解决方案**:

1. **`Package gl was not found` 或 `X11/Xlib.h: No such file or directory`**
   ```bash
   # 使用快速修复脚本 (推荐)
   ./fix_ubuntu_deps.sh

   # 或手动安装依赖
   sudo apt install -y build-essential pkg-config libgl1-mesa-dev \
       mesa-common-dev libx11-dev libglfw3-dev libgtk-3-dev
   ```

2. **一键安装所有依赖**:
   ```bash
   sudo apt update && sudo apt install -y build-essential pkg-config \
       libx11-dev libxcursor-dev libxrandr-dev libxinerama-dev \
       libxi-dev libxxf86vm-dev libxext-dev libxfixes-dev \
       libxrender-dev libxss-dev libgl1-mesa-dev libglu1-mesa-dev \
       mesa-common-dev libglfw3-dev libgtk-3-dev \
       libayatana-appindicator3-dev libxapp-dev
   ```

3. **Go 版本检查**: 确保 Go >= 1.21
   ```bash
   go version
   ```

详细故障排除指南请参考: [UBUNTU_MANUAL_FIX.md](UBUNTU_MANUAL_FIX.md)

#### Windows 构建问题

如果 GitHub Actions 构建的 Windows 版本无法运行:

1. **下载最新版本**: 确保下载最新的 release 版本
2. **检查系统兼容性**: 需要 Windows 10 或更高版本 (64位)
3. **杀毒软件**: 某些杀毒软件可能阻止未签名的可执行文件
4. **本地构建**: 如果仍有问题，可以本地构建:
   ```bash
   # Windows 本地构建
   make build-windows
   # 或手动构建
   CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags "-H windowsgui" -o Cursor_Windsurf_Reset.exe .
   ```

详细的 Windows 构建修复说明请参考:
- [WINDOWS_BUILD_FIX.md](WINDOWS_BUILD_FIX.md) - 本地构建问题修复
- [GITHUB_ACTIONS_WINDOWS_FIX.md](GITHUB_ACTIONS_WINDOWS_FIX.md) - GitHub Actions 构建修复

#### Usage Steps
1. After launching the application, the tool will automatically detect installed applications
2. Select the applications to reset (Cursor, Windsurf, or all)
3. Click the "Start Reset" button
4. Confirm the operation and wait for completion
5. View operation results and backup location

## 🛠️ Development Instructions

### Tech Stack
- **Language**: Go 1.21+
- **GUI Framework**: Fyne v2

### Project Structure
```
Cursor_Windsurf_Reset-go/
├── main.go                 # Main program entry
├── cleaner/
│   └── engine.go          # Cleaning engine core logic
├── config/
│   └── config.go          # Configuration management
├── gui/
│   ├── app.go             # GUI application main logic
│   ├── theme.go           # Theme definition
│   └── resources.go       # Resource files
├── reset_config.json      # Default configuration file
├── go.mod                 # Go module definition
└── README.md              # Project documentation
```

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🤝 Contributing

Issues and Pull Requests are welcome!

### Contributing Guidelines
1. Fork this repository
2. Create a feature branch (`git checkout -b feature/AmazingFeature`)
3. Commit your changes (`git commit -m 'Add some AmazingFeature'`)
4. Push to the branch (`git push origin feature/AmazingFeature`)
5. Open a Pull Request

## 📞 Support

- **GitHub Issues**: [Submit Issues](https://github.com/whispin/Cursor_Windsurf_Reset/issues)
- **Project Homepage**: [https://github.com/whispin/Cursor_Windsurf_Reset](https://github.com/whispin/Cursor_Windsurf_Reset)

## ⚠️ Disclaimer

This tool is for educational and research purposes only. When using this tool, please:

1. **Backup Data**: Please backup important data before use
2. **Follow Terms**: Comply with the terms of service of related applications
3. **Use at Your Own Risk**: Users assume all risks associated with using this tool
4. **Legal Use**: Ensure usage complies with local laws and regulations

---
