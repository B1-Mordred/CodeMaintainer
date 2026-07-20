CREATE TABLE notifications (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  id TEXT NOT NULL UNIQUE,
  kind TEXT NOT NULL CHECK(kind IN ('job_attention', 'job_completed', 'job_failed', 'job_cancelled', 'schedule_dispatched', 'schedule_skipped', 'review_requested', 'publication_requested')),
  project_id TEXT NOT NULL DEFAULT '',
  job_id TEXT NOT NULL DEFAULT '',
  schedule_id TEXT NOT NULL DEFAULT '',
  title TEXT NOT NULL,
  message TEXT NOT NULL,
  delivery TEXT NOT NULL CHECK(delivery = 'local_inbox'),
  state TEXT NOT NULL CHECK(state IN ('delivered', 'read')),
  created_at TEXT NOT NULL,
  read_at TEXT
) STRICT;

CREATE INDEX notifications_state_sequence_idx ON notifications(state, sequence DESC);

CREATE TABLE notification_events (
  sequence INTEGER PRIMARY KEY AUTOINCREMENT,
  notification_id TEXT NOT NULL REFERENCES notifications(id),
  action TEXT NOT NULL CHECK(action IN ('delivered', 'read')),
  actor_id TEXT NOT NULL,
  created_at TEXT NOT NULL
) STRICT;

INSERT INTO config_revisions(id, actor_id, schema_version, before_document, after_document,
  document_diff, validation_result, rollback_of, created_at)
SELECT 'config_notifications_v15', 'system-migration', schema_version, after_document,
  json_set(after_document, '$.notifications', json('{"local_inbox_enabled":true}')),
  '[{"op":"add","path":"/notifications","value":{"local_inbox_enabled":true}}]',
  '{"valid":true,"errors":[]}', '', strftime('%Y-%m-%dT%H:%M:%fZ', 'now')
FROM config_revisions
WHERE sequence = (SELECT MAX(sequence) FROM config_revisions)
  AND json_type(after_document, '$.notifications') IS NULL;

CREATE TRIGGER job_transition_notification
AFTER INSERT ON job_transitions
WHEN NEW.to_state IN ('awaiting_operator', 'completed', 'failed', 'cancelled')
  AND COALESCE((SELECT json_extract(after_document, '$.notifications.local_inbox_enabled')
    FROM config_revisions ORDER BY sequence DESC LIMIT 1), 1) = 1
BEGIN
  INSERT INTO notifications(id, kind, project_id, job_id, title, message, delivery, state, created_at)
  SELECT 'notification-job-transition-' || NEW.sequence,
    CASE NEW.to_state WHEN 'awaiting_operator' THEN 'job_attention' WHEN 'completed' THEN 'job_completed'
      WHEN 'failed' THEN 'job_failed' ELSE 'job_cancelled' END,
    jobs.project_id, NEW.job_id,
    CASE NEW.to_state WHEN 'awaiting_operator' THEN 'Job needs operator attention' WHEN 'completed' THEN 'Job completed'
      WHEN 'failed' THEN 'Job failed' ELSE 'Job cancelled' END,
    'Job ' || NEW.job_id || ' entered durable state ' || NEW.to_state || '.', 'local_inbox', 'delivered', NEW.created_at
  FROM jobs WHERE jobs.id = NEW.job_id;
  INSERT INTO notification_events(notification_id, action, actor_id, created_at)
  VALUES('notification-job-transition-' || NEW.sequence, 'delivered', 'controller', NEW.created_at);
END;

CREATE TRIGGER schedule_run_notification
AFTER INSERT ON schedule_runs
WHEN COALESCE((SELECT json_extract(after_document, '$.notifications.local_inbox_enabled')
  FROM config_revisions ORDER BY sequence DESC LIMIT 1), 1) = 1
BEGIN
  INSERT INTO notifications(id, kind, job_id, schedule_id, title, message, delivery, state, created_at)
  VALUES('notification-schedule-run-' || NEW.sequence,
    CASE NEW.status WHEN 'skipped' THEN 'schedule_skipped' ELSE 'schedule_dispatched' END,
    COALESCE(NEW.job_id, ''), NEW.schedule_id,
    CASE NEW.status WHEN 'skipped' THEN 'Scheduled task skipped' ELSE 'Scheduled task dispatched' END,
    'Schedule ' || NEW.schedule_id || ' recorded status ' || NEW.status || '.', 'local_inbox', 'delivered', NEW.created_at);
  INSERT INTO notification_events(notification_id, action, actor_id, created_at)
  VALUES('notification-schedule-run-' || NEW.sequence, 'delivered', 'controller-scheduler', NEW.created_at);
END;

CREATE TRIGGER automation_request_notification
AFTER INSERT ON automation_requests
WHEN COALESCE((SELECT json_extract(after_document, '$.notifications.local_inbox_enabled')
  FROM config_revisions ORDER BY sequence DESC LIMIT 1), 1) = 1
BEGIN
  INSERT INTO notifications(id, kind, job_id, title, message, delivery, state, created_at)
  VALUES('notification-automation-request-' || NEW.sequence,
    CASE NEW.kind WHEN 'review' THEN 'review_requested' ELSE 'publication_requested' END,
    NEW.job_id,
    CASE NEW.kind WHEN 'review' THEN 'Review requested' ELSE 'Publication approval requested' END,
    'A bounded automation request needs a human decision for job ' || NEW.job_id || '.',
    'local_inbox', 'delivered', NEW.created_at);
  INSERT INTO notification_events(notification_id, action, actor_id, created_at)
  VALUES('notification-automation-request-' || NEW.sequence, 'delivered', NEW.requested_by, NEW.created_at);
END;

CREATE TRIGGER notification_events_no_update BEFORE UPDATE ON notification_events BEGIN
  SELECT RAISE(ABORT, 'notification events are append-only');
END;
CREATE TRIGGER notification_events_no_delete BEFORE DELETE ON notification_events BEGIN
  SELECT RAISE(ABORT, 'notification events are append-only');
END;
CREATE TRIGGER notifications_no_delete BEFORE DELETE ON notifications BEGIN
  SELECT RAISE(ABORT, 'notifications cannot be deleted');
END;
