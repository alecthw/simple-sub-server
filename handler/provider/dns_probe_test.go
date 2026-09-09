package provider

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestBuildDNSQueryAndValidDNSAnswer(t *testing.T) {
	query, id, err := buildDNSQuery("node.airport.example.", 1)
	if err != nil {
		t.Fatal(err)
	}
	if got := binary.BigEndian.Uint16(query[0:2]); got != id {
		t.Fatalf("query id = %d, want %d", got, id)
	}
	if got := binary.BigEndian.Uint16(query[4:6]); got != 1 {
		t.Fatalf("question count = %d, want 1", got)
	}
	if got := binary.BigEndian.Uint16(query[len(query)-4 : len(query)-2]); got != 1 {
		t.Fatalf("query type = %d, want 1", got)
	}

	answer := successfulDNSResponse(query)
	if !validDNSAnswer(answer, id) {
		t.Fatal("valid DNS response was rejected")
	}

	wrongID := append([]byte(nil), answer...)
	binary.BigEndian.PutUint16(wrongID[0:2], id+1)
	if validDNSAnswer(wrongID, id) {
		t.Fatal("response with wrong id was accepted")
	}

	noAnswers := append([]byte(nil), answer...)
	binary.BigEndian.PutUint16(noAnswers[6:8], 0)
	if validDNSAnswer(noAnswers, id) {
		t.Fatal("response without answers was accepted")
	}

	failed := append([]byte(nil), answer...)
	binary.BigEndian.PutUint16(failed[2:4], 0x8182)
	if validDNSAnswer(failed, id) {
		t.Fatal("failed DNS response was accepted")
	}
}

func TestBuildDNSQueryRejectsInvalidDomain(t *testing.T) {
	if _, _, err := buildDNSQuery("", 1); err == nil {
		t.Fatal("empty domain was accepted")
	}
	if _, _, err := buildDNSQuery(strings.Repeat("a", 64)+".example", 1); err == nil {
		t.Fatal("oversized DNS label was accepted")
	}
}

func TestProbeDNSServerUDP(t *testing.T) {
	connection, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = connection.Close()
	})

	go serveUDPAnswers(connection)

	if !probeDNSServer("udp://"+connection.LocalAddr().String()+"#DIRECT", []string{"node.airport.example"}) {
		t.Fatal("available UDP DNS server was rejected")
	}
}

func TestProbeDNSServerDoHWithSelfSignedCertificate(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodPost {
			http.Error(w, "POST required", http.StatusMethodNotAllowed)
			return
		}
		query, err := io.ReadAll(request.Body)
		if err != nil {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/dns-message")
		_, _ = w.Write(successfulDNSResponse(query))
	}))
	defer server.Close()

	if !probeDNSServer(server.URL+"/dns-query#DIRECT&skip-cert-verify=true", []string{"node.airport.example"}) {
		t.Fatal("available self-signed DoH server was rejected")
	}
}

func TestProbeUDPDomainRejectsNonResponsiveServer(t *testing.T) {
	connection, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = connection.Close()
	}()

	contextWithTimeout, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	if probeUDPDomain(contextWithTimeout, connection.LocalAddr().String(), "node.airport.example") {
		t.Fatal("non-responsive UDP DNS server was accepted")
	}
}

func TestMergeAndPrioritizeDNSPolicies(t *testing.T) {
	const (
		sharedDNS      = "192.0.2.10#DIRECT"
		unavailableDNS = "192.0.2.11#DIRECT"
		publicDNS      = "1.0.0.1#DIRECT"
	)
	policies := []providerDNSPolicy{
		{
			name:         "first",
			domainRule:   "+.first.example",
			nameservers:  []string{unavailableDNS, sharedDNS},
			probeDomains: []string{"node.first.example"},
		},
		{
			name:         "second",
			domainRule:   "+.second.example",
			nameservers:  []string{sharedDNS, publicDNS},
			probeDomains: []string{"node.second.example"},
		},
		{
			name:         "unavailable-only",
			domainRule:   "+.removed.example",
			nameservers:  []string{unavailableDNS},
			probeDomains: []string{"node.removed.example"},
		},
	}

	var mutex sync.Mutex
	calls := make(map[string]int)
	probedDomains := make(map[string][]string)
	probe := func(nameserver string, domains []string) bool {
		identity := nameserverIdentity(nameserver)
		mutex.Lock()
		calls[identity]++
		probedDomains[identity] = append([]string(nil), domains...)
		mutex.Unlock()
		return identity == nameserverIdentity(sharedDNS) || identity == nameserverIdentity(publicDNS)
	}

	prioritized := mergeAndPrioritizeDNSPolicies(policies, probe)
	if len(prioritized) != 1 {
		t.Fatalf("prioritized policy count = %d, want 1: %#v", len(prioritized), prioritized)
	}
	for identity, count := range calls {
		if count != 1 {
			t.Fatalf("DNS identity %q probed %d times, want 1", identity, count)
		}
	}
	sharedDomains := probedDomains[nameserverIdentity(sharedDNS)]
	if !reflect.DeepEqual(sharedDomains, []string{"node.first.example", "node.second.example"}) {
		t.Fatalf("shared DNS probe domains = %#v", sharedDomains)
	}

	wantNameservers := []string{sharedDNS, publicDNS, unavailableDNS}
	if !reflect.DeepEqual(prioritized[0].nameservers, wantNameservers) {
		t.Fatalf("prioritized nameservers = %#v, want %#v", prioritized[0].nameservers, wantNameservers)
	}
	wantDomainRule := "+.first.example,+.second.example,+.removed.example"
	if prioritized[0].domainRule != wantDomainRule {
		t.Fatalf("merged domains = %q, want %q", prioritized[0].domainRule, wantDomainRule)
	}
}

func serveUDPAnswers(connection *net.UDPConn) {
	buffer := make([]byte, 64*1024)
	for {
		count, address, err := connection.ReadFromUDP(buffer)
		if err != nil {
			return
		}
		response := successfulDNSResponse(buffer[:count])
		_, _ = connection.WriteToUDP(response, address)
	}
}

func successfulDNSResponse(query []byte) []byte {
	if len(query) < 16 {
		return nil
	}
	response := append([]byte(nil), query...)
	binary.BigEndian.PutUint16(response[2:4], 0x8180)
	binary.BigEndian.PutUint16(response[6:8], 1)

	queryType := binary.BigEndian.Uint16(query[len(query)-4 : len(query)-2])
	response = append(response,
		0xc0, 0x0c,
		byte(queryType>>8), byte(queryType),
		0x00, 0x01,
		0x00, 0x00, 0x00, 0x1e,
	)
	if queryType == 28 {
		response = append(response, 0x00, 0x10)
		response = append(response, make([]byte, 16)...)
	} else {
		response = append(response, 0x00, 0x04, 127, 0, 0, 1)
	}
	return response
}
