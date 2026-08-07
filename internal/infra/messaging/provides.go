package messaging

import (
	domainmessaging "github.com/torchwooddev/torchwood/internal/domain/messaging"
	"github.com/google/wire"
)

var ProviderSet = wire.NewSet(
	NewMailer,
	NewSMSService,
	wire.Bind(new(domainmessaging.Mailer), new(*MailerService)),
	wire.Bind(new(domainmessaging.SMSSender), new(*SMSService)),
)
