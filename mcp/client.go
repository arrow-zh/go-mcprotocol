package mcp

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// 默认读写超时时间，防止网络波动导致长时间阻塞
const defaultTimeout = 5 * time.Second

type Client interface {
	Connect() error // 新增：主动建立连接
	Close() error   // 新增：主动关闭连接
	Read(deviceName string, offset, numPoints int64) ([]byte, error)
	BitRead(deviceName string, offset, numPoints int64) ([]byte, error)
	Write(deviceName string, offset, numPoints int64, writeData []byte) ([]byte, error)
	HealthCheck() error
}

// client3E is 3E frame mcp client
type client3E struct {
	// PLC address
	tcpAddr *net.TCPAddr
	// PLC station
	stn *station

	// 新增：保持长连接的对象
	conn *net.TCPConn
}

func New3EClient(host string, port int, stn *station) (Client, error) {
	tcpAddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%v:%v", host, port))
	if err != nil {
		return nil, err
	}
	return &client3E{tcpAddr: tcpAddr, stn: stn}, nil
}

// 内部方法：不加锁的连接逻辑（供内部方法调用）
func (c *client3E) connect() error {
	if c.conn != nil {
		return nil // 已经连接
	}

	conn, err := net.DialTimeout("tcp", c.tcpAddr.String(), 3*time.Second)
	if err != nil {
		return err
	}

	tcpConn, ok := conn.(*net.TCPConn)
	if !ok {
		conn.Close()
		return fmt.Errorf("failed to assert connection to *net.TCPConn")
	}

	// 3. 赋值给你原来的变量
	c.conn = tcpConn
	return nil
}

// 内部方法：不加锁的断开逻辑，发生错误时清理连接
func (c *client3E) disconnect() {
	if c.conn != nil {
		err := c.conn.Close()
		if err != nil {
			return
		}
		c.conn = nil
	}
}

// Connect 暴露给外部的主动连接方法
func (c *client3E) Connect() error {
	return c.connect()
}

// Close 暴露给外部的主动断开方法
func (c *client3E) Close() error {
	c.disconnect()
	return nil
}

// MELSECコミュニケーションプロトコル p180
// 11.4折返しテスト
func (c *client3E) HealthCheck() error {
	requestStr := c.stn.BuildHealthCheckRequest()

	payload, err := hex.DecodeString(requestStr)
	if err != nil {
		return err
	}

	// 自动重连
	if err := c.connect(); err != nil {
		return err
	}

	// 设置读写超时
	c.conn.SetDeadline(time.Now().Add(defaultTimeout))

	// Send message
	if _, err = c.conn.Write(payload); err != nil {
		c.disconnect() // 发送失败，断开连接，下次自动重连
		return err
	}

	// Receive message
	readBuff := make([]byte, 30)
	readLen, err := c.conn.Read(readBuff)
	if err != nil {
		c.disconnect()
		return err
	}

	resp := readBuff[:readLen]

	if readLen != 18 {
		return errors.New("plc connect test is fail: return length is [" + fmt.Sprintf("%X", resp) + "]")
	}

	// decodeString is 折返しデータ数ヘッダ[1byte]
	if "0500" != fmt.Sprintf("%X", resp[11:13]) {
		return errors.New("plc connect test is fail: return header is [" + fmt.Sprintf("%X", resp[11:13]) + "]")
	}

	//  折返しデータ[5byte]=ABCDE
	if "4142434445" != fmt.Sprintf("%X", resp[13:18]) {
		return errors.New("plc connect test is fail: return body is [" + fmt.Sprintf("%X", resp[13:18]) + "]")
	}

	return nil
}

// Read is send read as word command to remote plc by mc protocol
// Read 是底层 3E 帧二进制协议的读取实现
func (c *client3E) Read(deviceName string, offset, numPoints int64) ([]byte, error) {
	// 1. 构造请求报文
	requestStr := c.stn.BuildReadRequest(deviceName, offset, numPoints)
	payload, err := hex.DecodeString(requestStr)
	if err != nil {
		return nil, fmt.Errorf("hex decode error: %w", err)
	}

	// ============================================================
	// 核心改进 1：设置 2 秒绝对超时，防止掉包、断线导致的 goroutine 永久卡死
	// ============================================================
	// 设置读写截止时间为当前时间 + 2秒
	err = c.conn.SetDeadline(time.Now().Add(2 * time.Second))
	if err != nil {
		c.disconnect()
		return nil, fmt.Errorf("set deadline error: %w", err)
	}

	// 函数退出前，理论上应该重置 Deadline（视长连接管理策略而定）
	// defer c.conn.SetDeadline(time.Time{})

	// 2. 发送请求
	if _, err = c.conn.Write(payload); err != nil {
		c.disconnect()
		return nil, fmt.Errorf("conn write error: %w", err)
	}

	// ============================================================
	// 核心改进 2：分两步精准读取，彻底根除“256”字节偏移错位问题
	// ============================================================

	// 第一步：先严格读取前 9 个字节（MC协议固定报文头）
	// 包含：副标题(2), 网络号(1), PC号(1), I/O号(2), 站号(1), 数据长度(2)
	headerBuf := make([]byte, 9)
	_, err = io.ReadFull(c.conn, headerBuf)
	if err != nil {
		c.disconnect()
		return nil, fmt.Errorf("read header error (io.ReadFull): %w", err)
	}

	// 解析出 DataLength（位于 index 7-8，小端序）
	// 这个 DataLength 告诉了我们后面还有多少个字节（结束码2字节 + 真实数据）
	dataLen := binary.LittleEndian.Uint16(headerBuf[7:9])

	// 第二步：根据准确的长度，读取剩余的所有报文
	// 即使网络有轻微波动，io.ReadFull 也会在超时时间内等待字节凑齐
	restBuf := make([]byte, dataLen)
	_, err = io.ReadFull(c.conn, restBuf)
	if err != nil {
		c.disconnect()
		return nil, fmt.Errorf("read body error (io.ReadFull): %w", err)
	}

	// 3. 拼接头部和身体，返回给上层 parser 处理
	// 拼接后的完整切片结构：
	// [0:9]   Header (之前读的)
	// [9:11]  EndCode (在 restBuf 的起始位置)
	// [11:]   Payload (真正的寄存器数据)
	fullResponse := append(headerBuf, restBuf...)

	return fullResponse, nil
}

// BitRead is send read as bit command to remote plc by mc protocol
func (c *client3E) BitRead(deviceName string, offset, numPoints int64) ([]byte, error) {
	requestStr := c.stn.BuildBitReadRequest(deviceName, offset, numPoints)
	payload, err := hex.DecodeString(requestStr)
	if err != nil {
		return nil, err
	}

	// Send message
	if _, err = c.conn.Write(payload); err != nil {
		c.disconnect()
		return nil, err
	}

	// Receive message
	readBuff := make([]byte, 22+2*numPoints)
	readLen, err := c.conn.Read(readBuff)
	if err != nil {
		c.disconnect()
		return nil, err
	}

	return readBuff[:readLen], nil
}

// Write is send write command to remote plc by mc protocol
func (c *client3E) Write(deviceName string, offset, numPoints int64, writeData []byte) ([]byte, error) {
	requestStr := c.stn.BuildWriteRequest(deviceName, offset, numPoints, writeData)
	payload, err := hex.DecodeString(requestStr)
	if err != nil {
		return nil, err
	}

	// Send message
	if _, err = c.conn.Write(payload); err != nil {
		c.disconnect()
		return nil, err
	}

	// Receive message
	readBuff := make([]byte, 22)
	readLen, err := c.conn.Read(readBuff)
	if err != nil {
		c.disconnect()
		return nil, err
	}
	return readBuff[:readLen], nil
}
