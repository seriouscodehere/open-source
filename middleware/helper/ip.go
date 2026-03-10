// helper/ip.go
package helper

import (
	"net"
	"net/http"
	"strings"

	"middleware/base"
)

// IPExtractor implements base.IPExtractorInterface
type IPExtractor struct {
	Strategy      int
	TrustedNets   []*net.IPNet
	TrustedHeader string
}

// NewIPExtractor returns base.IPExtractorInterface
func NewIPExtractor(strategy int, trustedProxies []string, customHeader string) (base.IPExtractorInterface, error) {
	extractor := &IPExtractor{
		Strategy:      strategy,
		TrustedHeader: customHeader,
	}
	for _, cidr := range trustedProxies {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, err
		}
		extractor.TrustedNets = append(extractor.TrustedNets, ipNet)
	}
	return extractor, nil
}

// Extract implements base.IPExtractorInterface
func (e *IPExtractor) Extract(r *http.Request) string {
	remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if remoteIP == "" {
		remoteIP = r.RemoteAddr
	}

	switch e.Strategy {
	case 0: // StrategyDirect
		return remoteIP
	case 1: // StrategyCloudflare
		if cfIP := r.Header.Get("CF-Connecting-IP"); cfIP != "" {
			if net.ParseIP(cfIP) != nil {
				return cfIP
			}
		}
		return remoteIP
	case 2, 3: // StrategyTrustedProxy, StrategyAWS
		if !e.isTrusted(remoteIP) {
			return remoteIP
		}
		header := e.TrustedHeader
		if header == "" {
			header = "X-Forwarded-For"
		}
		xff := r.Header.Get(header)
		if xff == "" {
			return remoteIP
		}
		ips := strings.Split(xff, ",")
		for i := len(ips) - 1; i >= 0; i-- {
			ip := strings.TrimSpace(ips[i])
			if parsed := net.ParseIP(ip); parsed != nil {
				if i == len(ips)-1 || e.isTrusted(ip) {
					return ip
				}
			}
		}
		return remoteIP
	default:
		return remoteIP
	}
}

func (e *IPExtractor) isTrusted(ip string) bool {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return false
	}
	for _, net := range e.TrustedNets {
		if net.Contains(parsedIP) {
			return true
		}
	}
	return false
}

func NormalizeIP(ip string, enableIPv6Subnet bool, subnetSize int) string {
	parsedIP := net.ParseIP(ip)
	if parsedIP == nil {
		return ip
	}

	if enableIPv6Subnet && parsedIP.To4() == nil {
		if subnetSize < 48 || subnetSize > 128 {
			subnetSize = 56
		}
		ipNet := &net.IPNet{
			IP:   parsedIP.Mask(net.CIDRMask(subnetSize, 128)),
			Mask: net.CIDRMask(subnetSize, 128),
		}
		return ipNet.String()
	}

	return parsedIP.String()
}
