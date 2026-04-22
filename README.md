# go-mcprotocol

go-mcprotocol is a Go library for communicating with Mitsubishi PLCs via the MC Protocol (MELSEC Communication Protocol), supporting the 3E binary frame.

## Features

- 3E frame binary protocol communication over TCP
- Read / BitRead / Write PLC registers
- Health check (loopback test)
- Persistent TCP connection with auto-reconnect
- Response parsing
- Periodic data mirroring to CSV file

## Supported Devices

| Code | Device Type |
|------|-------------|
| `X`  | Input       |
| `Y`  | Output      |
| `M`  | Internal Relay |
| `L`  | Latch Relay |
| `F`  | Alarm       |
| `V`  | Edge Relay  |
| `B`  | Link Relay  |
| `W`  | Link Register |
| `D`  | Data Register |

## Usage

### Create Client

```go
import "github.com/arrow-zh/go-mcprotocol/mcp"

// Create a 3E client targeting a local station
client, err := mcp.New3EClient("192.168.0.1", 5000, mcp.NewLocalStation())
if err != nil {
    log.Fatal(err)
}

// Connect to PLC (auto-reconnects on subsequent calls)
if err := client.Connect(); err != nil {
    log.Fatal(err)
}
defer client.Close()
```

### Health Check

```go
if err := client.HealthCheck(); err != nil {
    log.Fatalf("failed health check for plc: %v", err)
}
```

### Read Registers (Word)

```go
// Read 3 word units starting from D100
raw, err := client.Read("D", 100, 3)
if err != nil {
    log.Fatal(err)
}

// Parse the raw MC protocol response
resp, err := mcp.NewParser().Do(raw)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("EndCode: %s, Payload: %X\n", resp.EndCode, resp.Payload)
```

### Bit Read

```go
// Read 5 bits starting from B0
raw, err := client.BitRead("B", 0, 5)
if err != nil {
    log.Fatal(err)
}
```

### Write Registers

```go
// Write 4 word units of data to D100
_, err := client.Write("D", 100, 4, []byte("test"))
if err != nil {
    log.Fatal(err)
}
```

### Response Parsing

The `Parser` decodes a raw MC protocol response into a structured `Response`:

| Field            | Description                  |
|------------------|------------------------------|
| `SubHeader`      | Sub header (fixed "5000")    |
| `NetworkNum`     | Network number               |
| `PCNum`          | PC number                    |
| `UnitIONum`      | Unit I/O number              |
| `UnitStationNum` | Unit station number          |
| `DataLen`        | Response data length         |
| `EndCode`        | Completion code ("0000" = OK)|
| `Payload`        | Register data                |
| `ErrInfo`        | Error information (if any)   |

### Station Configuration

`NewLocalStation()` returns a default local station configuration suitable for direct connections:

| Parameter        | Value  |
|------------------|--------|
| Network Number   | `00`   |
| PC Number        | `FF`   |
| Unit I/O Number  | `FF03` |
| Unit Station Num | `00`   |

For multi-drop connections, use `NewStation()` to specify custom values:

```go
stn := mcp.NewStation("00", "FF", "FF03", "01")
client, _ := mcp.New3EClient("192.168.0.1", 5000, stn)
```

## Mirroring Tool

The `mirror` package periodically reads PLC registers and writes the data to a CSV file (timestamp, Base64-encoded MC response).

Output format:

```csv
2019-10-07T07:08:00.3623052Z,0AAA//8DAAwAAAAAAAAAAAAAAAAA
2019-10-07T07:08:00.8622182Z,0AAA//8DAAwAAAAAAAAAAAAAAAAA
```

## Testing

Integration tests require a real PLC accessible over the network. Set environment variables to run them:

```bash
export PLC_TEST_HOST=192.168.0.1
export PLC_TEST_PORT=5000
go test ./...
```

Unit tests (no PLC required) can run without environment variables.

## Project Status

**Work In Progress** — Access route (`access_route.go`) is not yet implemented.

## License

Apache 2.0
