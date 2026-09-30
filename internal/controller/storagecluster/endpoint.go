package storagecluster

import (
	"net"
	"regexp"
	"time"
)

// checkEndpointReachable checks whether the given endpoint is reachable over TCP
// within the provided timeout. Any http:// or https:// scheme prefix is stripped
// before dialing. It is shared across modes (external cluster resource setup and
// KMS endpoint validation), so it deliberately lives outside the external-only files.
func checkEndpointReachable(endpoint string, timeout time.Duration) error {
	rxp := regexp.MustCompile(`^http[s]?://`)
	// remove any http or https protocols from the endpoint string
	endpoint = rxp.ReplaceAllString(endpoint, "")
	con, err := net.DialTimeout("tcp", endpoint, timeout)
	if err != nil {
		return err
	}
	defer func() { _ = con.Close() }()
	return nil
}
