package mcp

import (
	"encoding/hex"
	"errors"
	"fmt"
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
	conn, err := net.DialTCP("tcp", nil, c.tcpAddr)
	if err != nil {
		return err
	}
	c.conn = conn
	return nil
}

// 内部方法：不加锁的断开逻辑，发生错误时清理连接
func (c *client3E) disconnect() {
	if c.conn != nil {
		c.conn.Close()
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
func (c *client3E) Read(deviceName string, offset, numPoints int64) ([]byte, error) {
	requestStr := c.stn.BuildReadRequest(deviceName, offset, numPoints)
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
