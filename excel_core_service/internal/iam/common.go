// iam/common.go：模块内可复用逻辑归口。
package iam

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	commonv1 "excel/api/common/v1"
	iamv1 "excel/api/iam/v1"
	"excel_core_service/base"
	"excel_core_service/deal"
	redisstore "excel_core_service/redis"
)

// LinkResolver resolves a share link token without exposing share persistence to IAM.
type LinkResolver interface {
	ResolveLinkToken(ctx context.Context, token string) (commonv1.LinkContext, error)
}

// IamService implements the IAM RPCs delivered by ticket 04.
type IamService struct {
	iamv1.UnimplementedIamServiceServer

	repository *base.IAMRepository
	sessions   *redisstore.SessionStore
	links      LinkResolver
	now        func() time.Time
}

// NewIamService constructs the IAM service with its persistence boundaries.
func NewIamService(
	repository *base.IAMRepository,
	sessions *redisstore.SessionStore,
	links LinkResolver,
) *IamService {
	return &IamService{
		repository: repository,
		sessions:   sessions,
		links:      links,
		now:        time.Now,
	}
}

// Register installs an IAM service on a gRPC registrar.
func Register(registrar grpc.ServiceRegistrar, service *IamService) {
	iamv1.RegisterIamServiceServer(registrar, service)
}

func validateRegistration(username, password string) error {
	if !validUsername(username) || !validPassword(password) {
		return rpcError(deal.CodeInvalidArgument)
	}
	return nil
}

func validUsername(username string) bool {
	if len(username) < 4 || len(username) > 20 || username[0] < 'A' || (username[0] > 'Z' && username[0] < 'a') || username[0] > 'z' {
		return false
	}
	for _, character := range username[1:] {
		if !isASCIILetterOrDigit(character) && character != '_' {
			return false
		}
	}
	return true
}

func validPassword(password string) bool {
	count := utf8.RuneCountInString(password)
	if count < 8 || count > 32 {
		return false
	}
	var hasLetter, hasDigit bool
	for _, character := range password {
		hasLetter = hasLetter || isASCIILetter(character)
		hasDigit = hasDigit || (character >= '0' && character <= '9')
	}
	return hasLetter && hasDigit
}

func isASCIILetter(character rune) bool {
	return character <= unicode.MaxASCII && unicode.IsLetter(character)
}

func isASCIILetterOrDigit(character rune) bool {
	return isASCIILetter(character) || (character >= '0' && character <= '9')
}

func newSessionToken() (string, error) {
	random := make([]byte, 32)
	if _, err := rand.Read(random); err != nil {
		return "", err
	}
	return "sess_" + base64.RawURLEncoding.EncodeToString(random), nil
}

func protoUser(user base.IAMUser) *iamv1.User {
	return &iamv1.User{
		Id:           user.ID,
		Username:     user.Username,
		DisplayName:  user.DisplayName,
		AvatarUri:    user.AvatarURI,
		EnterpriseId: user.EnterpriseID,
		CreatedAt:    user.CreatedAt,
	}
}

func rpcError(code int) error {
	spec, _ := deal.LookupError(code)
	withDetails, err := status.New(spec.GRPCCode, spec.Message).WithDetails(&errdetails.ErrorInfo{
		Reason: strconv.Itoa(code),
	})
	if err != nil {
		return status.Error(spec.GRPCCode, spec.Message)
	}
	return withDetails.Err()
}

func isUsernameConflict(err error) bool {
	return errors.Is(err, base.ErrUsernameExists)
}

func sessionPrincipal(user base.IAMUser) *commonv1.Principal {
	principal := &commonv1.Principal{
		UserId:       user.ID,
		EnterpriseId: user.EnterpriseID,
		Internal:     user.Internal,
	}
	if user.LinkBaseID != "" || user.LinkToken != "" || user.LinkRole != "" {
		principal.LinkContext = &commonv1.LinkContext{
			BaseId:    user.LinkBaseID,
			LinkToken: user.LinkToken,
			Role:      user.LinkRole,
		}
	}
	return principal
}

func clientIP(ctx context.Context) string {
	if incoming, ok := metadata.FromIncomingContext(ctx); ok {
		for _, value := range incoming.Get("x-client-ip") {
			if address := strings.TrimSpace(value); address != "" {
				return address
			}
		}
	}
	if remote, ok := peer.FromContext(ctx); ok && remote.Addr != nil {
		host, _, err := net.SplitHostPort(remote.Addr.String())
		if err == nil {
			return host
		}
		return remote.Addr.String()
	}
	return "unknown"
}

func principalFromContext(ctx context.Context) (*commonv1.Principal, error) {
	incoming, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	values := incoming.Get("x-principal")
	if len(values) == 0 {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	payload, err := base64.StdEncoding.DecodeString(values[0])
	if err != nil {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	var principal commonv1.Principal
	if err := proto.Unmarshal(payload, &principal); err != nil || principal.GetUserId() == "" {
		return nil, rpcError(deal.CodeUnauthenticated)
	}
	return &principal, nil
}
