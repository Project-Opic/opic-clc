package runtimeconfig

import (
	"context"

	redisstore "github.com/sharptoolbox/opic-techbase/services/shared/pkg/redis"
	sharedruntimeconfig "github.com/sharptoolbox/opic-techbase/services/shared/pkg/runtimeconfig"
)

const RuntimeConfigInvalidationChannel = sharedruntimeconfig.InvalidationChannel

func PublishInvalidation(ctx context.Context, key string) error {
	return sharedruntimeconfig.InvalidationHandler{
		Channel: RuntimeConfigInvalidationChannel, IsSupported: isRuntimeConfigInvalidationKey, Refresh: RefreshByKey,
	}.Publish(ctx, key)
}

func RefreshByKey(ctx context.Context, key string) error {
	switch key {
	case SecurityPolicySettingKey:
		return DefaultSecurityPolicyReader().Refresh(ctx)
	case EmailNotificationSettingKey:
		return DefaultEmailNotificationReader().Refresh(ctx)
	default:
		return nil
	}
}

func StartInvalidationListener(ctx context.Context) (*redisstore.StringSubscriber, error) {
	return sharedruntimeconfig.InvalidationHandler{
		Channel: RuntimeConfigInvalidationChannel, IsSupported: isRuntimeConfigInvalidationKey, Refresh: RefreshByKey,
	}.Start(ctx)
}

func isRuntimeConfigInvalidationKey(key string) bool {
	switch key {
	case SecurityPolicySettingKey, EmailNotificationSettingKey:
		return true
	default:
		return false
	}
}
