//go:build linux

package discovery

import (
	"encoding/binary"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMatchesICMPProbe(t *testing.T) {
	const id, seq = uint16(42), uint16(7)
	echo := make([]byte, 8)
	echo[0] = 0
	binary.BigEndian.PutUint16(echo[4:6], id)
	binary.BigEndian.PutUint16(echo[6:8], seq)
	require.True(t, matchesICMPProbe(echo, id, seq))
	require.False(t, matchesICMPProbe(echo, id, seq+1))

	request := createICMPEchoRequest(int(seq))
	binary.BigEndian.PutUint16(request[4:6], id)
	quoted := make([]byte, 8+20+len(request))
	quoted[0] = 11
	quoted[8] = 0x45
	quoted[8+9] = 1
	copy(quoted[8+20:], request)
	require.True(t, matchesICMPProbe(quoted, id, seq))
	quoted[len(quoted)-1]++
	require.True(t, matchesICMPProbe(quoted, id, seq), "payload changes do not alter probe identity")
	binary.BigEndian.PutUint16(quoted[8+20+6:8+20+8], seq+1)
	require.False(t, matchesICMPProbe(quoted, id, seq))
}
