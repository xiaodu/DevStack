package process

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// 端口监听的唯一事实来源改为 iphlpapi!GetExtendedTcpTable：
//   - netstat.exe 是外部进程依赖，Windows 11 24H2+ 有被精简/禁用的先例（wmic 同款教训）；
//   - API 直接读内核 TCP 表，不起子进程、不解析文本，且天然返回同端口的全部 PID
//     （master/worker 都继承监听套接字，旧文本解析的行序问题一并消失）。
//
// FindAllPIDsByPort 优先走本文件；API 失败时才回退 netstat（保底可用）。

const (
	afInet  = 2
	afInet6 = 23

	// TCP_TABLE_OWNER_PID_LISTENER：仅 LISTEN 状态的行，附带属主 PID
	tcpTableOwnerPidListener = 3

	tcpStateListen = 2

	errorInsufficientBuffer = 122
)

// mibTCPRowOwnerPID 对应 MIB_TCPROW_OWNER_PID（24 字节）。
// LocalPort 以“网络字节序的 16 位端口放进低位 DWORD”的形式存储，需 ntohs。
type mibTCPRowOwnerPID struct {
	State      uint32
	LocalAddr  uint32
	LocalPort  uint32
	RemoteAddr uint32
	RemotePort uint32
	OwningPID  uint32
}

// mibTCP6RowOwnerPID 对应 MIB_TCP6ROW_OWNER_PID（48 字节）。
type mibTCP6RowOwnerPID struct {
	LocalAddr  [16]byte
	LocalPort  uint32
	RemoteAddr [16]byte
	RemotePort uint32
	State      uint32
	OwningPID  uint32
}

var (
	iphlpapi                = windows.NewLazySystemDLL("iphlpapi.dll")
	procGetExtendedTcpTable = iphlpapi.NewProc("GetExtendedTcpTable")
)

// ntohs16 把表项低位 DWORD 中网络字节序的端口转成主机序。
func ntohs16(v uint32) uint32 {
	return (v&0xFF)<<8 | (v>>8)&0xFF
}

// getTCPTableRaw 按地址族取监听表原始字节。两次调用：探大小 → 取数据。
func getTCPTableRaw(af uint32) ([]byte, error) {
	if err := procGetExtendedTcpTable.Find(); err != nil {
		return nil, fmt.Errorf("iphlpapi GetExtendedTcpTable 不可用: %w", err)
	}
	var size uint32
	// GetExtendedTcpTable 的 Win32 错误码在函数返回值(r1)里，不是 GetLastError
	rc, _, _ := procGetExtendedTcpTable.Call(
		0, uintptr(unsafe.Pointer(&size)), 0,
		uintptr(af), uintptr(tcpTableOwnerPidListener), 0)
	if rc == 0 {
		return nil, nil // 表为空
	}
	if rc != errorInsufficientBuffer || size == 0 {
		return nil, fmt.Errorf("探测 TCP 表(%d)大小失败: %v", af, rc)
	}
	buf := make([]byte, size)
	rc, _, _ = procGetExtendedTcpTable.Call(
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&size)), 0,
		uintptr(af), uintptr(tcpTableOwnerPidListener), 0)
	if rc != 0 {
		return nil, fmt.Errorf("读取 TCP 表(%d)失败: %v", af, rc)
	}
	return buf[:size], nil
}

// listeningPortsViaAPI 返回 port → 监听该端口的全部 PID（IPv4+IPv6 合并去重）。
// 注意：GetExtendedTcpTable 的监听表缓冲区是 MIB_TCPTABLE_OWNER_PID 结构——
// 开头 4 字节是 dwNumEntries，其后才是行数组；直接拿整个缓冲按行步进会把
// 计数头当成第一行、后续全部错位。
func listeningPortsViaAPI() (map[int][]int, error) {
	result := make(map[int][]int)
	seen := make(map[int]map[int]bool)
	add := func(port, pid int) {
		if port <= 0 || pid <= 0 {
			return
		}
		if seen[port] == nil {
			seen[port] = make(map[int]bool)
		}
		if !seen[port][pid] {
			seen[port][pid] = true
			result[port] = append(result[port], pid)
		}
	}

	v4, err4 := getTCPTableRaw(afInet)
	if err4 != nil {
		return nil, err4
	}
	const row4 = int(unsafe.Sizeof(mibTCPRowOwnerPID{}))
	if len(v4) >= 4 {
		count := int(*(*uint32)(unsafe.Pointer(&v4[0])))
		for i := 0; i < count; i++ {
			off := 4 + i*row4
			if off+row4 > len(v4) {
				break
			}
			row := *(*mibTCPRowOwnerPID)(unsafe.Pointer(&v4[off]))
			if row.State != tcpStateListen {
				continue
			}
			add(int(ntohs16(row.LocalPort)), int(row.OwningPID))
		}
	}

	v6, err6 := getTCPTableRaw(afInet6)
	if err6 != nil {
		return nil, err6
	}
	const row6 = int(unsafe.Sizeof(mibTCP6RowOwnerPID{}))
	if len(v6) >= 4 {
		count := int(*(*uint32)(unsafe.Pointer(&v6[0])))
		for i := 0; i < count; i++ {
			off := 4 + i*row6
			if off+row6 > len(v6) {
				break
			}
			row := *(*mibTCP6RowOwnerPID)(unsafe.Pointer(&v6[off]))
			if row.State != tcpStateListen {
				continue
			}
			add(int(ntohs16(row.LocalPort)), int(row.OwningPID))
		}
	}
	return result, nil
}
