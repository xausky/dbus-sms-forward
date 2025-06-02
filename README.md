# DBus短信转发工具

这是一个使用Go语言编写的工具，可以通过DBus监听短信接收并发送短信。该工具使用ModemManager DBus接口与调制解调器进行交互。

## 功能

- 监听短信接收并显示短信内容
- 发送短信到指定号码
- 根据配置规则转发短信到HTTP API

## 依赖

- 系统中安装并且启动了 ModemManager
- mmcli -m 0 查询 modem 状态为 state: registered 的时候才能收到或者发送短信

## 安装

从 Release 下载对应架构可执行程序放到设备上，使用 chmod +x dbus-sms-forward 给予可执行权限。

## 使用方法

### 监听短信接收

```bash
./dbus-sms-forward watch
```

运行此命令后，程序将自动检测系统中的调制解调器，并监听短信接收事件。当收到新短信时，会显示发送者号码、时间和内容。

### 发送短信

```bash
./dbus-sms-forward send -number="+1234567890" -text="这是一条测试短信"
```

参数说明：
- `-number`: 接收短信的电话号码
- `-text`: 短信内容

### 转发短信

```bash
./dbus-sms-forward forward [-config=config.yml]
```

参数说明：
- `-config`: 配置文件路径，默认为当前目录下的 config.yml

运行此命令后，程序将监听短信接收事件，并根据配置文件中的规则将匹配的短信转发到指定的HTTP API。

## 配置文件格式

配置文件使用YAML格式，示例如下：

```yaml
# 短信转发配置文件
forwards:
  # 转发规则1：将包含验证码的短信转发到指定URL
  - name: "验证码转发"
    number: ".*"                   # 匹配任何发送者号码
    text: ".*验证码.*|.*code.*"     # 匹配包含"验证码"或"code"的短信
    url: "https://example.com/api/sms/forward"
    body:
      type: "verification"
      phone: "{{number}}"
      message: "{{text}}"
      timestamp: "{{timestamp}}"

  # 转发规则2：将特定号码的短信转发到另一个URL
  - name: "银行短信转发"
    number: "^95588$|^95566$"      # 匹配银行号码
    text: ".*"                     # 匹配任何内容
    url: "https://myserver.com/banking/notify"
    body:
      source: "sms"
      bank_message: "{{text}}"
      sender: "{{number}}"
```

配置说明：
- `name`: 规则名称，便于识别
- `number`: 发送者号码的正则表达式
- `text`: 短信内容的正则表达式
- `url`: 转发目标URL
- `body`: POST请求的JSON体，支持以下占位符, Body 支持任意层次结构的数据：
  - `{{number}}`: 发送者号码
  - `{{text}}`: 短信内容
  - `{{timestamp}}`: 短信时间戳

## 工作原理

该工具使用Go的DBus库（github.com/godbus/dbus/v5）与系统的ModemManager服务通信。

- 当使用`watch`命令时，程序会监听ModemManager的`Added`信号，该信号在接收到新短信时会被触发
- 当使用`send`命令时，程序会使用ModemManager的`Create`和`Send`方法创建并发送短信
- 当使用`forward`命令时，程序会监听短信接收事件，并根据配置规则将匹配的短信转发到指定URL

## 常见问题

### 无法找到调制解调器

确保您的系统中有可用的调制解调器设备（如USB调制解调器或内置蜂窝模块），并且ModemManager服务正在运行。

```bash
# 检查ModemManager服务状态
systemctl status ModemManager
```

### 无法发送短信

确保您的调制解调器已正确配置，并且有可用的网络连接。某些调制解调器可能需要先解锁SIM卡PIN码。

### 转发规则不匹配

检查配置文件中的正则表达式是否正确。可以使用在线正则表达式测试工具进行验证。

## 许可证

MIT

## 参考资料

- [ModemManager DBus接口文档](https://www.freedesktop.org/software/ModemManager/doc/latest/ModemManager/gdbus-org.freedesktop.ModemManager1.Modem.Messaging.html)
- [godbus/dbus](https://github.com/godbus/dbus) 