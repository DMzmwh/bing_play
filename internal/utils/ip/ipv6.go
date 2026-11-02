package ip

import (
	"context"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

func HasGlobalIPv6() bool {
	ifaces, err := net.Interfaces()
	if err != nil {
		return false
	}

	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 {
			continue
		}

		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}

		for _, a := range addrs {
			ipNet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}

			ip := ipNet.IP

			if ip.To4() != nil {
				continue
			}

			if ip.IsLoopback() ||
				ip.IsLinkLocalUnicast() ||
				ip.IsLinkLocalMulticast() {
				continue
			}

			return true
		}
	}

	return false
}

// ipv6TestTargets 多个稳定的公网 IPv6 目标。
// 单目标不可达（部分网络只封某个站点、或该地址临时故障）时会误判为
// “应关闭 IPv6”，从而丢失 IPv6 对等节点，因此这里探测多个目标，任一可达即视为可用。
var ipv6TestTargets = []string{
	"[2606:4700:4700::1111]:443", // Cloudflare
	"[2606:4700:4700::1001]:443", // Cloudflare
	"[2001:4860:4860::8888]:443", // Google
}

func testIPv6(ctx context.Context) bool {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var ok atomic.Bool
	var wg sync.WaitGroup
	for _, target := range ipv6TestTargets {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()

			d := net.Dialer{}
			conn, err := d.DialContext(ctx, "tcp6", target)
			if err != nil {
				return
			}
			ok.Store(true)
			cancel()
			conn.Close()
		}(target)
	}
	wg.Wait()

	return ok.Load()
}

func testIPv4(ctx context.Context) bool {
	d := net.Dialer{}
	conn, err := d.DialContext(ctx, "tcp4", "1.1.1.1:53")
	if err != nil {
		return false
	}
	conn.Close()
	return true
}
func ShouldDisableIPv6() bool {
	if !HasGlobalIPv6() {
		fmt.Println("No global IPv6")
		return true
	}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()

	ipv6OK := testIPv6(ctx)
	fmt.Println("IPv6:", ipv6OK)
	return !ipv6OK
}

//func ShouldDisableIPv6() bool {
//
//	// 连IPv6地址都没有
//	if !HasGlobalIPv6() {
//		return true
//	}
//
//	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
//	defer cancel()
//
//	var wg sync.WaitGroup
//
//	var ipv6OK bool
//	var ipv4OK bool
//
//	wg.Add(2)
//
//	go func() {
//		defer wg.Done()
//		ipv6OK = testIPv6(ctx)
//	}()
//
//	go func() {
//		defer wg.Done()
//		ipv4OK = testIPv4(ctx)
//	}()
//
//	wg.Wait()
//
//	// IPv4正常但IPv6失败
//	if ipv4OK && !ipv6OK {
//		return true
//	}
//
//	return false
//}
