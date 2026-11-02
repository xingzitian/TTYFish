package api

import (
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

const gatewayHeader = "X-TTYFish-Gateway-Token"
const gatewayIPHeader = "X-TTYFish-Client-IP"

type sourceResolver struct{ proxies []netip.Prefix }

func newSourceResolver(cidrs []string) (sourceResolver, error) {
	var resolver sourceResolver
	for _, text := range cidrs {
		prefix, err := netip.ParsePrefix(strings.TrimSpace(text))
		if err != nil || prefix.Bits() == 0 {
			return sourceResolver{}, errors.New("可信代理必须使用明确的 CIDR，不能信任所有地址")
		}
		if prefix.Addr().Is4In6() {
			if prefix.Bits() < 96 {
				return sourceResolver{}, errors.New("可信代理的 IPv4 映射网段不合法")
			}
			prefix = netip.PrefixFrom(prefix.Addr().Unmap(), prefix.Bits()-96)
			if prefix.Bits() == 0 {
				return sourceResolver{}, errors.New("不能信任所有 IPv4 地址")
			}
		}
		resolver.proxies = append(resolver.proxies, prefix.Masked())
	}
	return resolver, nil
}
func parsePeer(remote string) netip.Addr {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		host = remote
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	return ip.WithZone("").Unmap()
}
func (s sourceResolver) trusted(ip netip.Addr) bool {
	for _, prefix := range s.proxies {
		if prefix.Contains(ip) {
			return true
		}
	}
	return false
}
func (s sourceResolver) ip(r *http.Request, gateway bool) (string, error) {
	if gateway && r.Header.Get(gatewayIPHeader) != "" {
		values := r.Header.Values(gatewayIPHeader)
		ip, err := netip.ParseAddr(strings.TrimSpace(values[0]))
		if err != nil || len(values) != 1 || ip.Zone() != "" {
			return "", errors.New("网关客户端 IP 必须为单个有效 IP 地址")
		}
		return ip.Unmap().String(), nil
	}
	peer := parsePeer(r.RemoteAddr)
	if !peer.IsValid() {
		return "", nil
	}
	direct := peer.String()
	if !s.trusted(peer) {
		return direct, nil
	}
	header := strings.Join(r.Header.Values("X-Forwarded-For"), ",")
	if header == "" {
		return direct, nil
	}
	chain := strings.Split(header, ",")
	if len(chain) > 32 {
		return direct, nil
	}
	// Work from the closest proxy backwards. Never select an arbitrary leftmost value.
	current := peer
	for i := len(chain) - 1; i >= 0 && s.trusted(current); i-- {
		next, err := netip.ParseAddr(strings.TrimSpace(chain[i]))
		if err != nil || next.Zone() != "" {
			return direct, nil
		}
		current = next.Unmap()
	}
	return current.String(), nil
}
