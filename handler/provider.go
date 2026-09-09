package handler

import "github.com/alecthw/sub-server/internal/provider"

// StartProviderDNSPolicyScheduler starts the daily proxy-dns.yml refresh.
func (s *Server) StartProviderDNSPolicyScheduler() {
	provider.StartProxyDNSPolicyScheduler(s.providerDir, s.client)
}
