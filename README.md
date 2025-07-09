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
