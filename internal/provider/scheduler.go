package provider

import (
	"time"

	"github.com/go-resty/resty/v2"
	"go.uber.org/zap"
)

// StartProxyDNSPolicyScheduler refreshes proxy-dns.yml every day at 03:00
// in the server's local timezone.
func StartProxyDNSPolicyScheduler(providerDir string, client *resty.Client) {
	go func() {
		timer := time.NewTimer(time.Until(nextProxyDNSRefresh(time.Now())))
		defer timer.Stop()
		for {
			<-timer.C
			if _, err := refreshProxyDNSPolicy(providerDir, client); err != nil {
				zap.S().Errorw("scheduled proxy dns policy refresh failed", "error", err)
			}
			timer.Reset(time.Until(nextProxyDNSRefresh(time.Now())))
		}
	}()
}

func nextProxyDNSRefresh(now time.Time) time.Time {
	next := time.Date(now.Year(), now.Month(), now.Day(), 3, 0, 0, 0, now.Location())
	if !next.After(now) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}
