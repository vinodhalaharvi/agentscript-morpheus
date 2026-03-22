// Package network provides pure-Go network diagnostic tools for AgentScript.
// No external dependencies — uses only stdlib: net, crypto/tls, net/http.
//
// Commands: ssl_check, ping, dns_lookup, port_check, whois, http_check, traceroute
package network

import (
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

const defaultTimeout = 10 * time.Second

// Client holds config for network diagnostics.
type Client struct {
	verbose bool
	timeout time.Duration
}

// NewClient creates a network diagnostic client.
func NewClient(verbose bool) *Client {
	return &Client{verbose: verbose, timeout: defaultTimeout}
}

// SSLCheck checks the SSL certificate for a host.
// Returns expiry, issuer, SANs, days remaining, and TLS version.
func (c *Client) SSLCheck(host string) (string, error) {
	// Strip scheme if present
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimSuffix(host, "/")

	// Add port if missing
	hostPort := host
	if !strings.Contains(host, ":") {
		hostPort = host + ":443"
	}

	conn, err := tls.DialWithDialer(
		&net.Dialer{Timeout: c.timeout},
		"tcp",
		hostPort,
		&tls.Config{ServerName: strings.Split(host, ":")[0]},
	)
	if err != nil {
		return "", fmt.Errorf("ssl_check failed for %s: %w", host, err)
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return "", fmt.Errorf("no certificates found for %s", host)
	}

	cert := certs[0]
	now := time.Now()
	daysRemaining := int(cert.NotAfter.Sub(now).Hours() / 24)

	status := "✅ VALID"
	if daysRemaining < 0 {
		status = "❌ EXPIRED"
	} else if daysRemaining < 7 {
		status = "🚨 CRITICAL - expires in " + fmt.Sprintf("%d days", daysRemaining)
	} else if daysRemaining < 30 {
		status = "⚠️  WARNING - expires in " + fmt.Sprintf("%d days", daysRemaining)
	}

	tlsVersion := tlsVersionString(conn.ConnectionState().Version)

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("SSL Certificate Report: %s\n", host))
	sb.WriteString(fmt.Sprintf("Status:        %s\n", status))
	sb.WriteString(fmt.Sprintf("Subject:       %s\n", cert.Subject.CommonName))
	sb.WriteString(fmt.Sprintf("Issuer:        %s\n", cert.Issuer.CommonName))
	sb.WriteString(fmt.Sprintf("Valid From:    %s\n", cert.NotBefore.Format("2006-01-02")))
	sb.WriteString(fmt.Sprintf("Valid Until:   %s\n", cert.NotAfter.Format("2006-01-02")))
	sb.WriteString(fmt.Sprintf("Days Remaining: %d days\n", daysRemaining))
	sb.WriteString(fmt.Sprintf("TLS Version:   %s\n", tlsVersion))
	if len(cert.DNSNames) > 0 {
		sb.WriteString(fmt.Sprintf("SANs:          %s\n", strings.Join(cert.DNSNames, ", ")))
	}

	return sb.String(), nil
}

// Ping checks if a host is reachable via TCP on port 80 or 443.
// Pure Go — no ICMP (requires root), uses TCP handshake latency instead.
func (c *Client) Ping(host string) (string, error) {
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimSuffix(host, "/")

	ports := []string{"443", "80", "22"}
	var results []string
	reachable := false

	for _, port := range ports {
		start := time.Now()
		conn, err := net.DialTimeout("tcp", host+":"+port, c.timeout)
		elapsed := time.Since(start)
		if err == nil {
			conn.Close()
			results = append(results, fmt.Sprintf("  port %s: ✅ reachable (%dms)", port, elapsed.Milliseconds()))
			reachable = true
		} else {
			results = append(results, fmt.Sprintf("  port %s: ❌ unreachable", port))
		}
	}

	status := "✅ REACHABLE"
	if !reachable {
		status = "❌ UNREACHABLE"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Ping Report: %s\n", host))
	sb.WriteString(fmt.Sprintf("Status: %s\n", status))
	sb.WriteString("Results:\n")
	for _, r := range results {
		sb.WriteString(r + "\n")
	}
	return sb.String(), nil
}

// DNSLookup resolves A, CNAME, MX, NS, and TXT records for a host.
func (c *Client) DNSLookup(host string) (string, error) {
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimSuffix(host, "/")

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("DNS Lookup: %s\n", host))

	// A records
	addrs, err := net.LookupHost(host)
	if err != nil {
		sb.WriteString(fmt.Sprintf("A Records:  ❌ NXDOMAIN - %v\n", err))
	} else {
		sb.WriteString(fmt.Sprintf("A Records:  %s\n", strings.Join(addrs, ", ")))
	}

	// CNAME
	cname, err := net.LookupCNAME(host)
	if err == nil && cname != host+"." {
		sb.WriteString(fmt.Sprintf("CNAME:      %s\n", cname))
	}

	// MX records
	mxs, err := net.LookupMX(host)
	if err == nil && len(mxs) > 0 {
		var mxList []string
		for _, mx := range mxs {
			mxList = append(mxList, fmt.Sprintf("%s (priority %d)", mx.Host, mx.Pref))
		}
		sb.WriteString(fmt.Sprintf("MX Records: %s\n", strings.Join(mxList, ", ")))
	}

	// NS records
	nss, err := net.LookupNS(host)
	if err == nil && len(nss) > 0 {
		var nsList []string
		for _, ns := range nss {
			nsList = append(nsList, ns.Host)
		}
		sb.WriteString(fmt.Sprintf("NS Records: %s\n", strings.Join(nsList, ", ")))
	}

	// TXT records
	txts, err := net.LookupTXT(host)
	if err == nil && len(txts) > 0 {
		sb.WriteString(fmt.Sprintf("TXT Records: %s\n", strings.Join(txts, "; ")))
	}

	return sb.String(), nil
}

// PortCheck checks if specific ports are open on a host.
// ports is a comma-separated list e.g. "80,443,8080"
func (c *Client) PortCheck(host, ports string) (string, error) {
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimSuffix(host, "/")

	portList := strings.Split(ports, ",")
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Port Check: %s\n", host))

	openCount := 0
	for _, port := range portList {
		port = strings.TrimSpace(port)
		start := time.Now()
		conn, err := net.DialTimeout("tcp", host+":"+port, c.timeout)
		elapsed := time.Since(start)
		if err == nil {
			conn.Close()
			sb.WriteString(fmt.Sprintf("  :%s  ✅ OPEN   (%dms)\n", port, elapsed.Milliseconds()))
			openCount++
		} else {
			sb.WriteString(fmt.Sprintf("  :%s  ❌ CLOSED\n", port))
		}
	}

	sb.WriteString(fmt.Sprintf("Summary: %d/%d ports open\n", openCount, len(portList)))
	return sb.String(), nil
}

// HTTPCheck checks HTTP/HTTPS response code, latency, and redirects.
func (c *Client) HTTPCheck(url string) (string, error) {
	if !strings.HasPrefix(url, "http") {
		url = "https://" + url
	}

	client := &http.Client{
		Timeout: c.timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("too many redirects")
			}
			return nil
		},
	}

	start := time.Now()
	resp, err := client.Get(url)
	elapsed := time.Since(start)
	if err != nil {
		return "", fmt.Errorf("http_check failed for %s: %w", url, err)
	}
	defer resp.Body.Close()

	status := "✅ OK"
	if resp.StatusCode >= 500 {
		status = "❌ SERVER ERROR"
	} else if resp.StatusCode >= 400 {
		status = "⚠️  CLIENT ERROR"
	} else if resp.StatusCode >= 300 {
		status = "↪️  REDIRECT"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("HTTP Check: %s\n", url))
	sb.WriteString(fmt.Sprintf("Status:     %s\n", status))
	sb.WriteString(fmt.Sprintf("HTTP Code:  %d %s\n", resp.StatusCode, http.StatusText(resp.StatusCode)))
	sb.WriteString(fmt.Sprintf("Latency:    %dms\n", elapsed.Milliseconds()))
	sb.WriteString(fmt.Sprintf("Final URL:  %s\n", resp.Request.URL.String()))
	if server := resp.Header.Get("Server"); server != "" {
		sb.WriteString(fmt.Sprintf("Server:     %s\n", server))
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		sb.WriteString(fmt.Sprintf("Content-Type: %s\n", ct))
	}
	return sb.String(), nil
}

// Whois does a basic WHOIS lookup via TCP to whois.iana.org then the registrar.
func (c *Client) Whois(domain string) (string, error) {
	domain = strings.TrimPrefix(domain, "https://")
	domain = strings.TrimPrefix(domain, "http://")
	domain = strings.TrimSuffix(domain, "/")
	// strip subdomain — whois needs apex domain
	parts := strings.Split(domain, ".")
	if len(parts) > 2 {
		domain = strings.Join(parts[len(parts)-2:], ".")
	}

	raw, err := whoisQuery("whois.iana.org", domain)
	if err != nil {
		return "", fmt.Errorf("whois failed: %w", err)
	}

	// Find refer: line to get the real whois server
	whoisServer := ""
	for _, line := range strings.Split(raw, "\n") {
		if strings.HasPrefix(strings.ToLower(line), "refer:") {
			whoisServer = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			break
		}
		if strings.HasPrefix(strings.ToLower(line), "whois:") {
			whoisServer = strings.TrimSpace(strings.SplitN(line, ":", 2)[1])
			break
		}
	}

	result := raw
	if whoisServer != "" {
		detailed, err := whoisQuery(whoisServer, domain)
		if err == nil {
			result = detailed
		}
	}

	// Extract key fields
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("WHOIS: %s\n", domain))

	keyFields := []string{
		"domain name", "registrar", "creation date", "registry expiry date",
		"updated date", "name server", "dnssec",
	}
	seen := map[string]bool{}
	for _, line := range strings.Split(result, "\n") {
		lower := strings.ToLower(line)
		for _, field := range keyFields {
			if strings.HasPrefix(lower, field+":") && !seen[field] {
				sb.WriteString(line + "\n")
				seen[field] = true
			}
		}
	}

	if sb.Len() < 20 {
		// fallback — return raw truncated
		lines := strings.Split(result, "\n")
		if len(lines) > 30 {
			lines = lines[:30]
		}
		return fmt.Sprintf("WHOIS: %s\n%s", domain, strings.Join(lines, "\n")), nil
	}

	return sb.String(), nil
}

// whoisQuery opens a TCP connection to a whois server and returns the response.
func whoisQuery(server, domain string) (string, error) {
	conn, err := net.DialTimeout("tcp", server+":43", 10*time.Second)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(10 * time.Second))
	fmt.Fprintf(conn, "%s\r\n", domain)
	var buf strings.Builder
	tmp := make([]byte, 4096)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf.Write(tmp[:n])
		}
		if err != nil {
			break
		}
	}
	return buf.String(), nil
}

// tlsVersionString converts a TLS version uint16 to a human-readable string.
func tlsVersionString(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("unknown (0x%x)", v)
	}
}
