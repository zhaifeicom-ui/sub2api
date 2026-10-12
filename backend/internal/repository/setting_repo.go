package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"github.com/Wei-Shaw/sub2api/ent"
	"github.com/Wei-Shaw/sub2api/ent/setting"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type settingRepository struct {
	client *ent.Client
	db     *sql.DB
}

func ProvideSettingRepository(client *ent.Client, db *sql.DB) service.SettingRepository {
	r := NewSettingRepository(client).(*settingRepository)
	r.db = db
	return r
}
func (r *settingRepository) RecordManagedPolicy(ctx context.Context, e service.ManagedPolicyEvent) error {
	if r.db == nil {
		return nil
	}
	ids, _ := json.Marshal(e.RuleIDs)
	var id int64
	err := r.db.QueryRowContext(ctx, `INSERT INTO managed_prompt_policy_events(request_id,group_id,config_version,status,protocol,rule_ids,input_bytes_before,input_bytes_after,transport_status) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`, e.RequestID, e.GroupID, e.Version, e.Status, e.Protocol, ids, e.Before, e.After, e.TransportStatus).Scan(&id)
	if err == nil && id%100 == 0 {
		_, _ = r.db.ExecContext(ctx, `DELETE FROM managed_prompt_policy_events WHERE id IN(SELECT id FROM managed_prompt_policy_events WHERE created_at<NOW()-INTERVAL '7 days' ORDER BY id LIMIT 500)`)
	}
	return err
}
func (r *settingRepository) ListManagedPolicy(ctx context.Context) ([]service.ManagedPolicyEvent, error) {
	out := []service.ManagedPolicyEvent{}
	if r.db == nil {
		return out, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT created_at,request_id,group_id,config_version,status,protocol,rule_ids,input_bytes_before,input_bytes_after,transport_status FROM managed_prompt_policy_events ORDER BY id DESC LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var e service.ManagedPolicyEvent
		var ids []byte
		if err := rows.Scan(&e.CreatedAt, &e.RequestID, &e.GroupID, &e.Version, &e.Status, &e.Protocol, &ids, &e.Before, &e.After, &e.TransportStatus); err != nil {
			return nil, err
		}
		_ = json.Unmarshal(ids, &e.RuleIDs)
		out = append(out, e)
	}
	return out, rows.Err()
}

func NewSettingRepository(client *ent.Client) service.SettingRepository {
	return &settingRepository{client: client}
}

func (r *settingRepository) Get(ctx context.Context, key string) (*service.Setting, error) {
	m, err := r.client.Setting.Query().Where(setting.KeyEQ(key)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, service.ErrSettingNotFound
		}
		return nil, err
	}
	return &service.Setting{
		ID:        m.ID,
		Key:       m.Key,
		Value:     m.Value,
		UpdatedAt: m.UpdatedAt,
	}, nil
}

func (r *settingRepository) GetValue(ctx context.Context, key string) (string, error) {
	setting, err := r.Get(ctx, key)
	if err != nil {
		return "", err
	}
	return setting.Value, nil
}

func (r *settingRepository) Set(ctx context.Context, key, value string) error {
	now := time.Now()
	return r.client.Setting.
		Create().
		SetKey(key).
		SetValue(value).
		SetUpdatedAt(now).
		OnConflictColumns(setting.FieldKey).
		UpdateNewValues().
		Exec(ctx)
}

func (r *settingRepository) CompareAndSetValue(ctx context.Context, key, old, value string) (bool, error) {
	if old == "" {
		err := r.client.Setting.Create().SetKey(key).SetValue(value).SetUpdatedAt(time.Now()).Exec(ctx)
		if ent.IsConstraintError(err) {
			return false, nil
		}
		return err == nil, err
	}
	n, err := r.client.Setting.Update().Where(setting.KeyEQ(key), setting.ValueEQ(old)).SetValue(value).SetUpdatedAt(time.Now()).Save(ctx)
	return n == 1, err
}

func (r *settingRepository) GetMultiple(ctx context.Context, keys []string) (map[string]string, error) {
	if len(keys) == 0 {
		return map[string]string{}, nil
	}
	settings, err := r.client.Setting.Query().Where(setting.KeyIn(keys...)).All(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, s := range settings {
		result[s.Key] = s.Value
	}
	return result, nil
}

func (r *settingRepository) SetMultiple(ctx context.Context, settings map[string]string) error {
	if len(settings) == 0 {
		return nil
	}

	now := time.Now()
	builders := make([]*ent.SettingCreate, 0, len(settings))
	for key, value := range settings {
		builders = append(builders, r.client.Setting.Create().SetKey(key).SetValue(value).SetUpdatedAt(now))
	}
	return r.client.Setting.
		CreateBulk(builders...).
		OnConflictColumns(setting.FieldKey).
		UpdateNewValues().
		Exec(ctx)
}

func (r *settingRepository) GetAll(ctx context.Context) (map[string]string, error) {
	settings, err := r.client.Setting.Query().All(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, s := range settings {
		result[s.Key] = s.Value
	}
	return result, nil
}

func (r *settingRepository) Delete(ctx context.Context, key string) error {
	_, err := r.client.Setting.Delete().Where(setting.KeyEQ(key)).Exec(ctx)
	return err
}
