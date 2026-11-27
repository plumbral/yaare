package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"reflect"
	"yaare/internal/domain/preferences"
	"yaare/internal/domain/workspace"
)

func invalidStoredPreferences() error {
	return workspace.Fail("PREFERENCES_CORRUPT", "设置记录无效，原记录未覆盖。")
}

func readPreferences(ctx context.Context, runner queryer) (preferences.Snapshot, bool, error) {
	current := preferences.Empty()
	var portable, device []byte
	err := runner.QueryRowContext(ctx, "SELECT version,revision,portable,device FROM desktop_preferences WHERE id=1").Scan(&current.Version, &current.Revision, &portable, &device)
	if err == sql.ErrNoRows {
		return current, false, nil
	}
	if err != nil {
		return current, false, err
	}
	if current.Version != preferences.Version || current.Revision < 1 {
		return current, false, invalidStoredPreferences()
	}
	if portable != nil {
		current.Portable, err = preferences.DecodePortable(portable)
		if err != nil {
			return current, false, invalidStoredPreferences()
		}
	}
	if device != nil {
		current.Device, err = preferences.DecodeDevice(device)
		if err != nil {
			return current, false, invalidStoredPreferences()
		}
	}
	return current, portable != nil, nil
}

func readPortablePreferences(ctx context.Context, runner queryer) (json.RawMessage, error) {
	var version int
	var raw []byte
	err := runner.QueryRowContext(ctx, "SELECT version,portable FROM desktop_preferences WHERE id=1").Scan(&version, &raw)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if version != preferences.Version {
		return nil, invalidStoredPreferences()
	}
	if raw == nil {
		return nil, nil
	}
	value, err := preferences.DecodePortable(raw)
	if err != nil {
		return nil, invalidStoredPreferences()
	}
	return json.Marshal(value)
}

func writePortablePreferences(ctx context.Context, tx *sql.Tx, raw json.RawMessage) error {
	if raw == nil {
		return nil
	}
	value, err := preferences.DecodePortable(raw)
	if err != nil {
		return err
	}
	next, err := json.Marshal(value)
	if err != nil {
		return err
	}
	current, err := readPortablePreferences(ctx, tx)
	if err != nil || bytes.Equal(current, next) {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO desktop_preferences(id,version,revision,portable) VALUES(1,?,1,?)
		ON CONFLICT(id) DO UPDATE SET portable=excluded.portable,revision=desktop_preferences.revision+1`, preferences.Version, next)
	return err
}

func (s *Store) Preferences(ctx context.Context) (preferences.Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return preferences.Snapshot{}, s.unavailable()
	}
	current, _, err := readPreferences(ctx, s.db)
	return current, err
}

func (s *Store) SavePreferences(ctx context.Context, request preferences.SaveRequest) (preferences.Snapshot, error) {
	if request.Version != preferences.Version || request.Revision < 0 ||
		(request.Device != nil && !preferences.ValidDevice(*request.Device)) ||
		(request.Portable == nil && request.Device == nil) {
		return preferences.Snapshot{}, preferences.Invalid()
	}
	var portable preferences.Portable
	if request.Portable != nil {
		var err error
		if portable, err = preferences.NormalizePortable(*request.Portable); err != nil {
			return preferences.Snapshot{}, err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.db == nil {
		return preferences.Snapshot{}, s.unavailable()
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return preferences.Snapshot{}, err
	}
	defer tx.Rollback()
	current, hadPortable, err := readPreferences(ctx, tx)
	if err != nil {
		return current, err
	}
	if request.Revision != current.Revision {
		return current, workspace.Fail("PREFERENCES_CONFLICT", "设置已更改，请重新读取后重试。")
	}
	next := current
	if request.Device != nil {
		next.Device = *request.Device
		if next.Device.Shortcuts == nil {
			next.Device.Shortcuts = map[string]*string{}
		}
	}
	hasPortable := hadPortable
	if request.Portable != nil {
		next.Portable = portable
		hasPortable = hadPortable || portable != (preferences.Portable{})
	}
	if reflect.DeepEqual(current, next) && hasPortable == hadPortable {
		return current, nil
	}
	next.Revision++
	if err = savePreferenceRecord(ctx, tx, next, hasPortable); err != nil {
		return current, storageWriteError(err, nil)
	}
	if current.Portable != next.Portable {
		var revision int64
		if err = tx.QueryRowContext(ctx, "UPDATE meta SET revision=revision+1 WHERE id=1 RETURNING revision").Scan(&revision); err != nil {
			return current, err
		}
		next.WorkspaceRevision = &preferences.RevisionChange{Before: revision - 1, After: revision}
	}
	if err = tx.Commit(); err != nil {
		return current, storageWriteError(err, nil)
	}
	if current.Portable != next.Portable {
		s.notifyLocalChangeLocked()
	}
	s.scheduleRoutineBackup(s.now())
	return next, nil
}

func savePreferenceRecord(ctx context.Context, tx *sql.Tx, value preferences.Snapshot, hasPortable bool) error {
	var portable []byte
	var err error
	if hasPortable {
		portable, err = json.Marshal(value.Portable)
		if err != nil {
			return err
		}
	}
	device, err := json.Marshal(value.Device)
	if err != nil {
		return err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO desktop_preferences(id,version,revision,portable,device) VALUES(1,?,?,?,?)
		ON CONFLICT(id) DO UPDATE SET revision=excluded.revision,portable=excluded.portable,device=excluded.device`, value.Version, value.Revision, portable, device)
	return err
}
