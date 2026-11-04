// comment/capacity_test.go：评论 2000 字容量软上限边界（契约 C-08，5001）。
package comment_test

import (

	"strings"
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	commentv1 "excel/api/comment/v1"
)

func TestCreateCommentEnforcesTwoThousandRuneCapacity(t *testing.T) {
	harness := newCommentHarness(t)
	ctx := commentPrincipal(t, harness.userA)

	exactlyLimit := strings.Repeat("评", 2000)
	ok, err := harness.client.CreateComment(ctx, &commentv1.CreateCommentRequest{
		RecordId: harness.recordID,
		Content:  exactlyLimit,
	})
	if err != nil {
		t.Fatalf("CreateComment(2000 runes) error = %v", err)
	}
	if ok.GetComment().GetId() == "" {
		t.Fatal("CreateComment(2000 runes) returned no comment")
	}

	overLimit := strings.Repeat("评", 2001)
	_, err = harness.client.CreateComment(ctx, &commentv1.CreateCommentRequest{
		RecordId: harness.recordID,
		Content:  overLimit,
	})
	if err == nil {
		t.Fatal("CreateComment(2001 runes) error = nil, want 5001")
	}
	grpcStatus, fromStatus := status.FromError(err)
	if !fromStatus || grpcStatus.Code() != codes.ResourceExhausted {
		t.Fatalf("CreateComment(2001 runes) code = %v, want ResourceExhausted(5001)", grpcStatus.Code())
	}
	if !strings.Contains(grpcStatus.Message(), "容量") {
		t.Fatalf("message = %q, want capacity wording", grpcStatus.Message())
	}
}
