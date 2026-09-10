package clients

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	clientv1 "github.com/mireacrm/contracts-go/mirea/client/v1"

	"github.com/mireacrm/go-common/infra"
	"github.com/mireacrm/notification-service/internal/notify"
)

var channelFromProto = map[clientv1.PreferredChannel]notify.Channel{
	clientv1.PreferredChannel_PREFERRED_CHANNEL_SMS:      notify.ChannelSMS,
	clientv1.PreferredChannel_PREFERRED_CHANNEL_EMAIL:    notify.ChannelEmail,
	clientv1.PreferredChannel_PREFERRED_CHANNEL_TELEGRAM: notify.ChannelTelegram,
}

type Clients struct {
	stub clientv1.ClientServiceClient
	conn *grpc.ClientConn
}

func DialClients(addr string) (*Clients, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("client: %w", err)
	}
	return &Clients{stub: clientv1.NewClientServiceClient(conn), conn: conn}, nil
}

func (c *Clients) Close() {
	if c.conn != nil {
		_ = c.conn.Close()
	}
}

func (c *Clients) ForClient(
	ctx context.Context, clientID uuid.UUID,
) (notify.ClientContacts, error) {
	response, err := c.stub.GetClientContacts(infra.Outgoing(ctx),
		&clientv1.GetClientContactsRequest{ClientId: clientID.String()})
	if err != nil {
		return notify.ClientContacts{}, translate(err, clientID)
	}

	contacts := response.GetContacts()
	preferred, ok := channelFromProto[contacts.GetPreferred()]
	if !ok {
		preferred = notify.ChannelSMS
	}

	return notify.ClientContacts{
		FullName:  contacts.GetFullName(),
		Phone:     contacts.GetPhone(),
		Email:     contacts.GetEmail(),
		Telegram:  contacts.GetTelegram(),
		Preferred: preferred,
	}, nil
}

func translate(err error, clientID uuid.UUID) error {
	switch status.Code(err) {
	case codes.PermissionDenied:
		return infra.Forbidden("client")
	case codes.NotFound:
		return infra.NotFound("client", clientID)
	case codes.InvalidArgument:
		return infra.InvalidArgument("%s", status.Convert(err).Message())
	case codes.Unavailable, codes.DeadlineExceeded:
		return infra.Unavailable("client", err)
	default:
		return err
	}
}
