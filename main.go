package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/godbus/dbus/v5"
	"gopkg.in/yaml.v3"
)

const (
	// ModemManager DBus 接口
	modemManagerService     = "org.freedesktop.ModemManager1"
	modemManagerPath        = "/org/freedesktop/ModemManager1"
	modemManagerInterface   = "org.freedesktop.ModemManager1"
	modemInterface          = "org.freedesktop.ModemManager1.Modem"
	modemMessagingInterface = "org.freedesktop.ModemManager1.Modem.Messaging"
	smsInterface            = "org.freedesktop.ModemManager1.Sms"
	dbusObjectManagerIface  = "org.freedesktop.DBus.ObjectManager"
)

// 短信信息结构
type SmsInfo struct {
	Number string
	Text   string
	Time   string
}

// 转发规则配置
type ForwardRule struct {
	Name   string                 `yaml:"name"`
	Number string                 `yaml:"number"`
	Text   string                 `yaml:"text"`
	URL    string                 `yaml:"url"`
	Body   map[string]interface{} `yaml:"body"`
}

// 配置文件结构
type Config struct {
	Forwards []ForwardRule `yaml:"forwards"`
}

func main() {
	// 定义子命令
	watchCmd := flag.NewFlagSet("watch", flag.ExitOnError)

	sendCmd := flag.NewFlagSet("send", flag.ExitOnError)
	phoneNumber := sendCmd.String("number", "", "接收短信的电话号码")
	messageText := sendCmd.String("text", "", "短信内容")

	forwardCmd := flag.NewFlagSet("forward", flag.ExitOnError)
	configFile := forwardCmd.String("config", "config.yml", "转发配置文件路径")

	// 检查命令行参数
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// 根据子命令选择对应的功能
	switch os.Args[1] {
	case "watch":
		watchCmd.Parse(os.Args[2:])
		watchSMS()
	case "send":
		sendCmd.Parse(os.Args[2:])
		if *phoneNumber == "" || *messageText == "" {
			fmt.Println("错误: 发送短信需要指定电话号码和短信内容")
			sendCmd.PrintDefaults()
			os.Exit(1)
		}
		sendSMS(*phoneNumber, *messageText)
	case "forward":
		forwardCmd.Parse(os.Args[2:])
		forwardSMS(*configFile)
	default:
		printUsage()
		os.Exit(1)
	}
}

// 打印使用说明
func printUsage() {
	fmt.Println("用法: dbus-sms-forward <命令> [参数]")
	fmt.Println("\n命令:")
	fmt.Println("  watch                  监听短信接收")
	fmt.Println("  send -number=<号码> -text=<内容>   发送短信")
	fmt.Println("  forward [-config=<配置文件路径>]   监听并转发短信")
}

// 监听短信接收
func watchSMS() {
	// 连接到系统 DBus
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		log.Fatalf("无法连接到系统 DBus: %v", err)
	}
	defer conn.Close()

	// 获取当前可用的调制解调器
	modems, err := listModems(conn)
	if err != nil {
		log.Printf("获取调制解调器列表失败: %v", err)
	} else {
		fmt.Printf("找到 %d 个调制解调器\n", len(modems))
		for i, path := range modems {
			fmt.Printf("[%d] %s\n", i, path)

			// 为每个调制解调器添加信号监听
			err = conn.AddMatchSignal(
				dbus.WithMatchObjectPath(path),
				dbus.WithMatchInterface(modemMessagingInterface),
				dbus.WithMatchMember("Added"),
			)
			if err != nil {
				log.Printf("无法为调制解调器 %s 添加信号匹配规则: %v", path, err)
			}
		}
	}

	// 如果没有找到调制解调器，添加一个通用的匹配规则
	if len(modems) == 0 {
		err = conn.AddMatchSignal(
			dbus.WithMatchInterface(modemMessagingInterface),
			dbus.WithMatchMember("Added"),
		)
		if err != nil {
			log.Fatalf("无法添加信号匹配规则: %v", err)
		}
	}

	// 创建信号通道
	signalChan := make(chan *dbus.Signal, 10)
	conn.Signal(signalChan)

	fmt.Println("开始监听短信接收...")
	fmt.Println("按 Ctrl+C 退出")

	// 创建退出信号通道
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 主循环
	for {
		select {
		case sig := <-signalChan:
			if sig.Name == modemMessagingInterface+".Added" {
				// 处理短信接收信号
				handleSmsAddedSignal(conn, sig)
			}
		case <-quit:
			fmt.Println("正在退出...")
			return
		}
	}
}

// 监听并转发短信
func forwardSMS(configPath string) {
	// 读取配置文件
	config, err := loadConfig(configPath)
	if err != nil {
		log.Fatalf("读取配置文件失败: %v", err)
	}

	fmt.Printf("已加载 %d 条转发规则\n", len(config.Forwards))

	// 连接到系统 DBus
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		log.Fatalf("无法连接到系统 DBus: %v", err)
	}
	defer conn.Close()

	// 获取当前可用的调制解调器
	modems, err := listModems(conn)
	if err != nil {
		log.Printf("获取调制解调器列表失败: %v", err)
	} else {
		fmt.Printf("找到 %d 个调制解调器\n", len(modems))
		for i, path := range modems {
			fmt.Printf("[%d] %s\n", i, path)

			// 为每个调制解调器添加信号监听
			err = conn.AddMatchSignal(
				dbus.WithMatchObjectPath(path),
				dbus.WithMatchInterface(modemMessagingInterface),
				dbus.WithMatchMember("Added"),
			)
			if err != nil {
				log.Printf("无法为调制解调器 %s 添加信号匹配规则: %v", path, err)
			}
		}
	}

	// 如果没有找到调制解调器，添加一个通用的匹配规则
	if len(modems) == 0 {
		err = conn.AddMatchSignal(
			dbus.WithMatchInterface(modemMessagingInterface),
			dbus.WithMatchMember("Added"),
		)
		if err != nil {
			log.Fatalf("无法添加信号匹配规则: %v", err)
		}
	}

	// 创建信号通道
	signalChan := make(chan *dbus.Signal, 10)
	conn.Signal(signalChan)

	fmt.Println("开始监听短信接收并根据规则转发...")
	fmt.Println("按 Ctrl+C 退出")

	// 创建退出信号通道
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	// 主循环
	for {
		select {
		case sig := <-signalChan:
			if sig.Name == modemMessagingInterface+".Added" {
				// 处理并转发短信
				handleSmsForward(conn, sig, config)
			}
		case <-quit:
			fmt.Println("正在退出...")
			return
		}
	}
}

// 读取配置文件
func loadConfig(path string) (*Config, error) {
	data, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("无法读取配置文件: %w", err)
	}

	var config Config
	err = yaml.Unmarshal(data, &config)
	if err != nil {
		return nil, fmt.Errorf("解析配置文件失败: %w", err)
	}

	return &config, nil
}

// 处理短信转发
func handleSmsForward(conn *dbus.Conn, sig *dbus.Signal, config *Config) {
	if len(sig.Body) < 2 {
		log.Println("无效的信号格式")
		return
	}

	// 从信号中获取短信路径
	smsPath, ok := sig.Body[0].(dbus.ObjectPath)
	if !ok {
		log.Println("无法获取短信路径")
		return
	}

	// 检查是否为接收的短信
	received, ok := sig.Body[1].(bool)
	if !ok {
		log.Println("无法确定短信是否为接收")
		return
	}

	if !received {
		// 忽略非接收的短信
		return
	}

	// 获取短信内容
	smsInfo, err := getSmsInfo(conn, smsPath)
	if err != nil {
		log.Printf("获取短信内容失败: %v", err)
		return
	}

	// 输出短信信息
	fmt.Println("收到新短信:")
	fmt.Printf("发送者: %s\n", smsInfo.Number)
	fmt.Printf("时间: %s\n", smsInfo.Time)
	fmt.Printf("内容: %s\n", smsInfo.Text)
	fmt.Println("------------------------")

	// 应用转发规则
	for _, rule := range config.Forwards {
		// 检查号码是否匹配
		numberMatched, err := regexp.MatchString(rule.Number, smsInfo.Number)
		if err != nil {
			log.Printf("号码正则匹配错误 [%s]: %v", rule.Name, err)
			continue
		}

		// 检查内容是否匹配
		textMatched, err := regexp.MatchString(rule.Text, smsInfo.Text)
		if err != nil {
			log.Printf("内容正则匹配错误 [%s]: %v", rule.Name, err)
			continue
		}

		// 如果号码和内容都匹配，执行转发
		if numberMatched && textMatched {
			fmt.Printf("匹配到转发规则: %s\n", rule.Name)
			err = forwardSmsToUrl(rule, smsInfo)
			if err != nil {
				log.Printf("转发失败 [%s]: %v", rule.Name, err)
			} else {
				fmt.Printf("成功转发到 %s\n", rule.URL)
			}
		}
	}
}

// 将短信转发到指定URL
func forwardSmsToUrl(rule ForwardRule, smsInfo SmsInfo) error {
	// 先将body序列化为JSON字符串
	jsonData, err := json.Marshal(rule.Body)
	if err != nil {
		return fmt.Errorf("JSON序列化失败: %w", err)
	}

	// 在JSON字符串中替换占位符
	jsonStr := string(jsonData)
	jsonStr = strings.ReplaceAll(jsonStr, "{{number}}", smsInfo.Number)
	jsonStr = strings.ReplaceAll(jsonStr, "{{text}}", smsInfo.Text)
	jsonStr = strings.ReplaceAll(jsonStr, "{{timestamp}}", smsInfo.Time)

	// 创建HTTP请求
	req, err := http.NewRequest("POST", rule.URL, bytes.NewBufferString(jsonStr))
	if err != nil {
		return fmt.Errorf("创建HTTP请求失败: %w", err)
	}

	// 设置请求头
	req.Header.Set("Content-Type", "application/json")

	// 发送请求
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("发送HTTP请求失败: %w", err)
	}
	defer resp.Body.Close()

	// 检查响应状态
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("HTTP请求返回非成功状态码: %d, 响应内容: %s", resp.StatusCode, string(body))
	}

	return nil
}

// 发送短信
func sendSMS(number, text string) {
	// 连接到系统 DBus
	conn, err := dbus.ConnectSystemBus()
	if err != nil {
		log.Fatalf("无法连接到系统 DBus: %v", err)
	}
	defer conn.Close()

	// 获取可用的调制解调器
	modems, err := listModems(conn)
	if err != nil {
		log.Fatalf("获取调制解调器列表失败: %v", err)
	}

	if len(modems) == 0 {
		log.Fatalf("没有找到可用的调制解调器")
	}

	// 使用第一个可用的调制解调器
	modemPath := modems[0]
	fmt.Printf("使用调制解调器: %s\n", modemPath)

	// 构建短信属性
	properties := map[string]dbus.Variant{
		"number": dbus.MakeVariant(number),
		"text":   dbus.MakeVariant(text),
	}

	// 调用Create方法创建短信
	obj := conn.Object(modemManagerService, modemPath)
	var smsPath dbus.ObjectPath
	err = obj.Call(modemMessagingInterface+".Create", 0, properties).Store(&smsPath)
	if err != nil {
		log.Fatalf("创建短信失败: %v", err)
	}

	fmt.Printf("短信已创建: %s\n", smsPath)

	// 发送短信
	smsObj := conn.Object(modemManagerService, smsPath)
	err = smsObj.Call(smsInterface+".Send", 0).Store()
	if err != nil {
		log.Fatalf("发送短信失败: %v", err)
	}

	fmt.Println("短信已成功发送!")
}

// 列出所有可用的调制解调器
func listModems(conn *dbus.Conn) ([]dbus.ObjectPath, error) {
	var modems []dbus.ObjectPath

	// 获取所有托管对象
	obj := conn.Object(modemManagerService, modemManagerPath)
	var managedObjects map[dbus.ObjectPath]map[string]map[string]dbus.Variant

	err := obj.Call(dbusObjectManagerIface+".GetManagedObjects", 0).Store(&managedObjects)
	if err != nil {
		return nil, fmt.Errorf("获取托管对象失败: %w", err)
	}

	// 过滤出调制解调器对象
	for path, interfaces := range managedObjects {
		if _, ok := interfaces[modemInterface]; ok {
			modems = append(modems, path)
		}
	}

	return modems, nil
}

// 处理短信接收信号
func handleSmsAddedSignal(conn *dbus.Conn, sig *dbus.Signal) {
	if len(sig.Body) < 2 {
		log.Println("无效的信号格式")
		return
	}

	// 从信号中获取短信路径
	smsPath, ok := sig.Body[0].(dbus.ObjectPath)
	if !ok {
		log.Println("无法获取短信路径")
		return
	}

	// 检查是否为接收的短信
	received, ok := sig.Body[1].(bool)
	if !ok {
		log.Println("无法确定短信是否为接收")
		return
	}

	if !received {
		// 忽略非接收的短信
		return
	}

	// 获取短信内容
	smsInfo, err := getSmsInfo(conn, smsPath)
	if err != nil {
		log.Printf("获取短信内容失败: %v", err)
		return
	}

	// 输出短信信息
	fmt.Println("收到新短信:")
	fmt.Printf("发送者: %s\n", smsInfo.Number)
	fmt.Printf("时间: %s\n", smsInfo.Time)
	fmt.Printf("内容: %s\n", smsInfo.Text)
	fmt.Println("------------------------")
}

// 获取短信内容
func getSmsInfo(conn *dbus.Conn, smsPath dbus.ObjectPath) (SmsInfo, error) {
	var smsInfo SmsInfo

	obj := conn.Object(modemManagerService, smsPath)

	// 获取短信属性
	variant, err := obj.GetProperty(smsInterface + ".Number")
	if err != nil {
		return smsInfo, fmt.Errorf("获取短信号码失败: %w", err)
	}
	smsInfo.Number = variant.Value().(string)

	variant, err = obj.GetProperty(smsInterface + ".Text")
	if err != nil {
		return smsInfo, fmt.Errorf("获取短信内容失败: %w", err)
	}
	smsInfo.Text = variant.Value().(string)

	variant, err = obj.GetProperty(smsInterface + ".Timestamp")
	if err != nil {
		return smsInfo, fmt.Errorf("获取短信时间戳失败: %w", err)
	}
	smsInfo.Time = variant.Value().(string)

	return smsInfo, nil
}
