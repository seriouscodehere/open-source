// base/extractor.go
package base

import (
	"fmt"
	"net"
	"net/http"
	"strings"
)

// IPExtractorInterface defines the interface for IP extraction
// This allows different implementations (base.IPExtractor struct or helper.IPExtractor)
type IPExtractorInterface interface {
	Extract(r *http.Request) string
}

// IPExtractor extracts client IP from HTTP requests
type IPExtractor struct {
	Strategy      IPExtractorStrategy
	TrustedNets   []*net.IPNet
	TrustedHeader string
}

// NewIPExtractor creates a new IP extractor
func NewIPExtractor(strategy IPExtractorStrategy, trustedProxies []string, customHeader string) (*IPExtractor, error) {
	extractor := &IPExtractor{
		Strategy:      strategy,
		TrustedHeader: customHeader,
	}
	for _, cidr := range trustedProxies {
		_, ipNet, err := net.ParseCIDR(cidr)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %s: %w", cidr, err)
		}
		extractor.TrustedNets = append(extractor.TrustedNets, ipNet)
	}
	return extractor, nil
}

// Extract extracts IP from request - implements IPExtractorInterface
func (e *IPExtractor) Extract(r *http.Request) string {
	remoteIP, _, _ := net.SplitHostPort(r.RemoteAddr)
	if remoteIP == "" {
		remoteIP = r.RemoteAddr
	}

	switch e.Strategy {
	case StrategyDirect:
		return remoteIP
	case StrategyCloudflare:
		if cfIP := r.Header.Get("CF-Connecting-IP"); cfIP != "" {
			if net.ParseIP(cfIP) != nil {
				return cfIP
			}
		}
		return remoteIP
	case StrategyTrustedProxy, StrategyAWS:
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

// NormalizeIP normalizes IP, optionally grouping IPv6 by subnet
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
