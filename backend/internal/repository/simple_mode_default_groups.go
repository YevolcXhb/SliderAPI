package repository

import (
	"context"
	"fmt"

	dbent "github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/group"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

const simpleModeDefaultGroupDescription = "Auto-created default group"

func ensureSimpleModeDefaultGroups(ctx context.Context, client *dbent.Client) error {
	if client == nil {
		return fmt.Errorf("nil ent client")
	}

	if err := backfillSimpleModeGrokDefaultImageGeneration(ctx, client); err != nil {
		return err
	}
	if err := backfillSimpleModeDefaultGroupsSubscriptionType(ctx, client); err != nil {
		return err
	}
	if err := backfillLegacyAntigravityNumberedDefaults(ctx, client); err != nil {
		return err
	}

	requiredByPlatform := map[string]int{
		service.PlatformAnthropic:   1,
		service.PlatformOpenAI:      1,
		service.PlatformGemini:      1,
		service.PlatformAntigravity: 1,
		service.PlatformGrok:        1,
		service.PlatformKiro:        1,
	}

	for platform, minCount := range requiredByPlatform {
		count, err := client.Group.Query().
			Where(group.PlatformEQ(platform), group.DeletedAtIsNil()).
			Count(ctx)
		if err != nil {
			return fmt.Errorf("count groups for platform %s: %w", platform, err)
		}

		// Each platform gets a single "<platform>-default" group.
		if count < minCount {
			name := platform + "-default"
			if err := createGroupIfNotExists(ctx, client, name, platform); err != nil {
				return err
			}
		}
	}

	return nil
}

func createGroupIfNotExists(ctx context.Context, client *dbent.Client, name, platform string) error {
	exists, err := client.Group.Query().
		Where(group.NameEQ(name), group.DeletedAtIsNil()).
		Exist(ctx)
	if err != nil {
		return fmt.Errorf("check group exists %s: %w", name, err)
	}
	if exists {
		return nil
	}

	_, err = client.Group.Create().
		SetName(name).
		SetDescription(simpleModeDefaultGroupDescription).
		SetPlatform(platform).
		SetStatus(service.StatusActive).
		SetSubscriptionType(service.SubscriptionTypeSubscription).
		SetRateMultiplier(1.0).
		SetIsExclusive(false).
		SetAllowImageGeneration(platform == service.PlatformGrok).
		Save(ctx)
	if err != nil {
		if dbent.IsConstraintError(err) {
			// Concurrent server startups may race on creation; treat as success.
			return nil
		}
		return fmt.Errorf("create default group %s: %w", name, err)
	}
	return nil
}

func backfillSimpleModeGrokDefaultImageGeneration(ctx context.Context, client *dbent.Client) error {
	_, err := client.Group.Update().
		Where(
			group.NameEQ(service.PlatformGrok+"-default"),
			group.PlatformEQ(service.PlatformGrok),
			group.DescriptionEQ(simpleModeDefaultGroupDescription),
			group.StatusEQ(service.StatusActive),
			group.AllowImageGenerationEQ(false),
			group.DeletedAtIsNil(),
		).
		SetAllowImageGeneration(true).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("backfill auto-created grok default image generation: %w", err)
	}
	return nil
}

// backfillSimpleModeDefaultGroupsSubscriptionType migrates existing auto-created
// default groups (created as "standard" by older builds) to "subscription" type,
// matching the current default. Only touches groups with the auto-created
// description so user-created groups are never changed.
func backfillSimpleModeDefaultGroupsSubscriptionType(ctx context.Context, client *dbent.Client) error {
	_, err := client.Group.Update().
		Where(
			group.DescriptionEQ(simpleModeDefaultGroupDescription),
			group.SubscriptionTypeEQ(service.SubscriptionTypeStandard),
			group.DeletedAtIsNil(),
		).
		SetSubscriptionType(service.SubscriptionTypeSubscription).
		Save(ctx)
	if err != nil {
		return fmt.Errorf("backfill auto-created default groups subscription type: %w", err)
	}
	return nil
}

// backfillLegacyAntigravityNumberedDefaults disables the legacy numbered
// antigravity default groups ("antigravity-default-1", "antigravity-default-2")
// that older builds created before antigravity switched to a single
// "antigravity-default" group. These are auto-created defaults (matched by
// description); operator-managed groups are never touched. The legacy groups
// are disabled (not deleted) so any accounts already bound to them keep working
// until an operator reassigns them.
func backfillLegacyAntigravityNumberedDefaults(ctx context.Context, client *dbent.Client) error {
	legacyNames := []string{
		service.PlatformAntigravity + "-default-1",
		service.PlatformAntigravity + "-default-2",
	}
	for _, name := range legacyNames {
		_, err := client.Group.Update().
			Where(
				group.NameEQ(name),
				group.DescriptionEQ(simpleModeDefaultGroupDescription),
				group.StatusEQ(service.StatusActive),
				group.DeletedAtIsNil(),
			).
			SetStatus(service.StatusDisabled).
			Save(ctx)
		if err != nil {
			return fmt.Errorf("disable legacy antigravity default %s: %w", name, err)
		}
	}
	return nil
}
