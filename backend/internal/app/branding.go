package app

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"sub2api-giftcode/backend/internal/models"
)

const (
	defaultBrandTitle   = "sub2api"
	defaultBrandSubtext = "兑换码系统"

	siteBrandTitleKey        = "site_brand_title"
	siteBrandSubtitleKey     = "site_brand_subtitle"
	siteMailSubjectPrefixKey = "site_mail_subject_prefix"
)

func defaultSiteBranding() *models.SiteBranding {
	return &models.SiteBranding{
		Title:    defaultBrandTitle,
		Subtitle: defaultBrandSubtext,
	}
}

func normalizeSiteBranding(branding models.SiteBranding) models.SiteBranding {
	title := strings.TrimSpace(branding.Title)
	if title == "" {
		title = defaultBrandTitle
	}
	subtitle := strings.TrimSpace(branding.Subtitle)
	if subtitle == "" {
		subtitle = defaultBrandSubtext
	}
	return models.SiteBranding{
		Title:             title,
		Subtitle:          subtitle,
		MailSubjectPrefix: strings.TrimSpace(branding.MailSubjectPrefix),
	}
}

func effectiveMailSubjectPrefix(title, subjectPrefix string) string {
	prefix := strings.TrimSpace(subjectPrefix)
	if prefix != "" {
		return prefix
	}
	title = strings.TrimSpace(title)
	if title == "" {
		title = defaultBrandTitle
	}
	return fmt.Sprintf("[%s]", title)
}

func (s *Service) GetSiteBranding(ctx context.Context) (*models.SiteBranding, error) {
	title, err := s.getSyncState(ctx, siteBrandTitleKey)
	if err != nil && !errorsIsNoRows(err) {
		return nil, err
	}
	subtitle, err := s.getSyncState(ctx, siteBrandSubtitleKey)
	if err != nil && !errorsIsNoRows(err) {
		return nil, err
	}
	prefix, err := s.getSyncState(ctx, siteMailSubjectPrefixKey)
	if err != nil && !errorsIsNoRows(err) {
		return nil, err
	}
	branding := normalizeSiteBranding(models.SiteBranding{
		Title:             title,
		Subtitle:          subtitle,
		MailSubjectPrefix: prefix,
	})
	return &branding, nil
}

func (s *Service) ReplaceSiteBranding(ctx context.Context, branding models.SiteBranding) (*models.SiteBranding, error) {
	normalized := normalizeSiteBranding(branding)
	now := s.now()

	tx, err := s.db().BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = tx.Rollback()
	}()
	rollback := func(err error) error {
		_ = tx.Rollback()
		return err
	}
	if err := upsertSyncStateTx(ctx, tx, siteBrandTitleKey, normalized.Title, now); err != nil {
		return nil, rollback(err)
	}
	if err := upsertSyncStateTx(ctx, tx, siteBrandSubtitleKey, normalized.Subtitle, now); err != nil {
		return nil, rollback(err)
	}
	if err := upsertSyncStateTx(ctx, tx, siteMailSubjectPrefixKey, normalized.MailSubjectPrefix, now); err != nil {
		return nil, rollback(err)
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &normalized, nil
}

func upsertSyncStateTx(ctx context.Context, tx *sql.Tx, key, value string, updatedAt time.Time) error {
	_, err := tx.ExecContext(ctx, `
INSERT INTO sync_state (key, value, updated_at) VALUES (?, ?, ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at
`, key, value, formatTime(updatedAt))
	return err
}

func errorsIsNoRows(err error) bool {
	return err == sql.ErrNoRows
}
