package kernel

import "testing"

func TestInsufficientCreditsKeepsStatusAndExposesReason(t *testing.T) {
	err := InsufficientCredits("积分不足，请先使用兑换码充值")
	if err.Status != 400 || err.Code != 400 || err.Reason != "insufficient_credits" || err.Error() != "积分不足，请先使用兑换码充值" {
		t.Fatalf("err = %#v", err)
	}
}
