package mail

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApprovalEmailIncludesLink(t *testing.T) {
	m := New(Config{SubjectPrefix: "[demo]"})
	subject, body := m.ApprovalEmail("Acme Billing", "[Acme]", 42, "alice", "alice@example.com", "$120", 120, 95, "please", "http://localhost/confirm?token=abc")
	require.Equal(t, "[Acme] 兑换申请审批 #42", subject)
	require.Contains(t, body, "Acme Billing 兑换申请审批")
	require.Contains(t, body, "alice@example.com")
	require.Contains(t, body, "alice")
	require.Contains(t, body, "please")
	require.Contains(t, body, "$120")
	require.Contains(t, body, "120")
	require.Contains(t, body, "95")
	require.Contains(t, body, "确认页")
	require.Contains(t, body, "不会立即处理申请")
	require.Contains(t, body, "http://localhost/confirm?token=abc")
}
