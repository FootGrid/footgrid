CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS citext;

CREATE SCHEMA identity;
CREATE SCHEMA competition;
CREATE SCHEMA match_data;
CREATE SCHEMA read_model;
CREATE SCHEMA platform;

CREATE TYPE platform.membership_role AS ENUM ('OWNER', 'ADMIN', 'ORGANIZER', 'TEAM_MANAGER', 'SCORER', 'REFEREE', 'PLAYER', 'FAN');
CREATE TYPE platform.membership_status AS ENUM ('INVITED', 'ACTIVE', 'SUSPENDED', 'REMOVED');
CREATE TYPE platform.match_status AS ENUM ('DRAFT', 'SCHEDULED', 'READY', 'LIVE', 'PAUSED', 'COMPLETED', 'FINALIZED', 'ABANDONED');
CREATE TYPE platform.match_side AS ENUM ('HOME', 'AWAY');
CREATE TYPE platform.participation_status AS ENUM ('STARTER', 'BENCH', 'NOT_SELECTED', 'LEFT_MATCH');
CREATE TYPE platform.clock_state AS ENUM ('NOT_STARTED', 'RUNNING', 'PAUSED', 'ENDED');
CREATE TYPE platform.event_state AS ENUM ('CONFIRMED', 'REVERSED');

CREATE OR REPLACE FUNCTION platform.touch_updated_at()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at = clock_timestamp();
  RETURN NEW;
END;
$$;

CREATE TABLE identity.organizations (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  name varchar(120) NOT NULL CHECK (char_length(btrim(name)) >= 2),
  slug citext NOT NULL UNIQUE CHECK (slug ~ '^[a-z0-9-]{3,80}$'),
  timezone varchar(64) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER organizations_touch BEFORE UPDATE ON identity.organizations FOR EACH ROW EXECUTE FUNCTION platform.touch_updated_at();

CREATE TABLE identity.users (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  cognito_subject uuid NOT NULL UNIQUE,
  display_name varchar(100) NOT NULL,
  phone_ciphertext bytea,
  phone_lookup_hash bytea UNIQUE,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER users_touch BEFORE UPDATE ON identity.users FOR EACH ROW EXECUTE FUNCTION platform.touch_updated_at();

CREATE TABLE identity.organization_memberships (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES identity.organizations(id),
  user_id uuid REFERENCES identity.users(id),
  role platform.membership_role NOT NULL,
  status platform.membership_status NOT NULL DEFAULT 'INVITED',
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE (organization_id, user_id)
);
CREATE INDEX organization_memberships_permission_ix ON identity.organization_memberships (organization_id, user_id, role) WHERE status = 'ACTIVE';
CREATE TRIGGER memberships_touch BEFORE UPDATE ON identity.organization_memberships FOR EACH ROW EXECUTE FUNCTION platform.touch_updated_at();

CREATE TABLE identity.player_profiles (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL REFERENCES identity.organizations(id),
  user_id uuid REFERENCES identity.users(id),
  display_name varchar(100) NOT NULL,
  phone_ciphertext bytea,
  phone_lookup_hash bytea,
  preferred_positions varchar(16)[] NOT NULL DEFAULT '{}',
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE UNIQUE INDEX player_profiles_org_phone_uq ON identity.player_profiles (organization_id, phone_lookup_hash) WHERE phone_lookup_hash IS NOT NULL;
CREATE TRIGGER player_profiles_touch BEFORE UPDATE ON identity.player_profiles FOR EACH ROW EXECUTE FUNCTION platform.touch_updated_at();

CREATE TABLE competition.teams (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL,
  name varchar(100) NOT NULL,
  short_name varchar(20),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE (organization_id, name)
);
CREATE TRIGGER teams_touch BEFORE UPDATE ON competition.teams FOR EACH ROW EXECUTE FUNCTION platform.touch_updated_at();

CREATE TABLE competition.venues (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL,
  name varchar(120) NOT NULL,
  timezone varchar(64) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TRIGGER venues_touch BEFORE UPDATE ON competition.venues FOR EACH ROW EXECUTE FUNCTION platform.touch_updated_at();

CREATE TABLE match_data.matches (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  organization_id uuid NOT NULL,
  venue_id uuid,
  venue_name_snapshot varchar(120),
  format_code varchar(16) NOT NULL CHECK (format_code IN ('5V5', '6V6', '8V8', '11V11', 'CUSTOM')),
  players_per_side smallint NOT NULL CHECK (players_per_side BETWEEN 2 AND 11),
  period_count smallint NOT NULL CHECK (period_count BETWEEN 1 AND 4),
  total_duration_seconds integer NOT NULL CHECK (total_duration_seconds BETWEEN 600 AND 10800),
  status platform.match_status NOT NULL DEFAULT 'DRAFT',
  status_version integer NOT NULL DEFAULT 0,
  created_by_member_id uuid,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  CHECK ((format_code = 'CUSTOM') OR (format_code = concat(players_per_side, 'V', players_per_side)))
);
CREATE INDEX matches_org_updated_ix ON match_data.matches (organization_id, updated_at DESC);
CREATE TRIGGER matches_touch BEFORE UPDATE ON match_data.matches FOR EACH ROW EXECUTE FUNCTION platform.touch_updated_at();

CREATE TABLE match_data.match_sides (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  match_id uuid NOT NULL REFERENCES match_data.matches(id) ON DELETE CASCADE,
  side platform.match_side NOT NULL,
  team_id uuid,
  display_name varchar(100) NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE (match_id, side),
  UNIQUE (id, match_id)
);

CREATE TABLE match_data.match_participants (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  match_id uuid NOT NULL REFERENCES match_data.matches(id) ON DELETE CASCADE,
  match_side_id uuid NOT NULL,
  player_id uuid,
  shirt_number smallint NOT NULL CHECK (shirt_number BETWEEN 1 AND 99),
  display_name_snapshot varchar(100) NOT NULL,
  position_code varchar(16),
  participation_status platform.participation_status NOT NULL DEFAULT 'NOT_SELECTED',
  pitch_slot varchar(32),
  joined_sequence integer NOT NULL DEFAULT 0,
  left_sequence integer,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  UNIQUE (id, match_id),
  FOREIGN KEY (match_side_id, match_id) REFERENCES match_data.match_sides(id, match_id)
);
CREATE UNIQUE INDEX match_participant_shirt_uq ON match_data.match_participants (match_side_id, shirt_number) WHERE participation_status <> 'LEFT_MATCH';
CREATE INDEX match_participants_match_ix ON match_data.match_participants (match_id, match_side_id, participation_status);
CREATE TRIGGER participants_touch BEFORE UPDATE ON match_data.match_participants FOR EACH ROW EXECUTE FUNCTION platform.touch_updated_at();

CREATE TABLE match_data.match_live_state (
  match_id uuid PRIMARY KEY REFERENCES match_data.matches(id) ON DELETE CASCADE,
  current_sequence integer NOT NULL DEFAULT 0 CHECK (current_sequence >= 0),
  home_score smallint NOT NULL DEFAULT 0 CHECK (home_score >= 0),
  away_score smallint NOT NULL DEFAULT 0 CHECK (away_score >= 0),
  current_period_number smallint,
  elapsed_seconds integer NOT NULL DEFAULT 0 CHECK (elapsed_seconds >= 0),
  clock_state platform.clock_state NOT NULL DEFAULT 'NOT_STARTED',
  active_scorer_member_id uuid,
  updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE match_data.live_sessions (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  match_id uuid NOT NULL REFERENCES match_data.matches(id) ON DELETE CASCADE,
  active_scorer_member_id uuid,
  status platform.clock_state NOT NULL DEFAULT 'RUNNING',
  period_number smallint NOT NULL DEFAULT 1,
  started_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  paused_at timestamptz,
  ended_at timestamptz,
  UNIQUE NULLS NOT DISTINCT (match_id, ended_at)
);
CREATE UNIQUE INDEX live_sessions_open_uq ON match_data.live_sessions (match_id) WHERE ended_at IS NULL;

CREATE TABLE match_data.action_catalog (
  code varchar(64) NOT NULL,
  version integer NOT NULL DEFAULT 1,
  display_label varchar(100) NOT NULL,
  score_delta smallint NOT NULL DEFAULT 0 CHECK (score_delta BETWEEN -1 AND 1),
  required_subject_roles varchar(24)[] NOT NULL DEFAULT ARRAY['PRIMARY'],
  qualifier_schema jsonb NOT NULL DEFAULT '{}'::jsonb,
  is_active boolean NOT NULL DEFAULT true,
  PRIMARY KEY (code, version)
);
INSERT INTO match_data.action_catalog (code, display_label, score_delta, required_subject_roles) VALUES
  ('GOAL', 'Goal', 1, ARRAY['SCORER']),
  ('ASSIST', 'Assist', 0, ARRAY['PRIMARY']),
  ('SHOT_ON_TARGET', 'Shot on target', 0, ARRAY['PRIMARY']),
  ('TACKLE_WON', 'Tackle won', 0, ARRAY['PRIMARY','OPPONENT']),
  ('INTERCEPTION', 'Interception', 0, ARRAY['PRIMARY']),
  ('GOALKEEPER_SAVE', 'Save', 0, ARRAY['PRIMARY']),
  ('FOUL_COMMITTED', 'Foul', 0, ARRAY['PRIMARY','OPPONENT']),
  ('FOUL_WON', 'Fouled', 0, ARRAY['PRIMARY','OPPONENT']),
  ('SUBSTITUTION', 'Substitution', 0, ARRAY['PLAYER_ON','PLAYER_OFF']),
  ('SCORE_ADJUSTMENT', 'Score adjustment', 0, ARRAY['PRIMARY']),
  ('EVENT_REVERSAL', 'Event reversal', 0, ARRAY['PRIMARY']);

CREATE TABLE match_data.match_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  match_id uuid NOT NULL REFERENCES match_data.matches(id) ON DELETE RESTRICT,
  client_event_id uuid NOT NULL,
  sequence integer NOT NULL CHECK (sequence > 0),
  action_code varchar(64) NOT NULL,
  action_catalog_version integer NOT NULL DEFAULT 1,
  side platform.match_side NOT NULL,
  period_number smallint NOT NULL DEFAULT 1,
  match_elapsed_seconds integer NOT NULL DEFAULT 0,
  recorded_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  recorded_by_member_id uuid,
  qualifiers jsonb NOT NULL DEFAULT '{}'::jsonb,
  state platform.event_state NOT NULL DEFAULT 'CONFIRMED',
  UNIQUE (match_id, sequence),
  UNIQUE (match_id, client_event_id),
  UNIQUE (id, match_id),
  FOREIGN KEY (action_code, action_catalog_version) REFERENCES match_data.action_catalog (code, version)
);
CREATE INDEX match_events_ledger_ix ON match_data.match_events (match_id, sequence);

CREATE TABLE match_data.match_event_subjects (
  match_id uuid NOT NULL,
  event_id uuid NOT NULL,
  participant_id uuid NOT NULL,
  role varchar(24) NOT NULL CHECK (role IN ('PRIMARY', 'SCORER', 'ASSISTER', 'OPPONENT', 'PLAYER_ON', 'PLAYER_OFF')),
  ordinal smallint NOT NULL DEFAULT 1,
  PRIMARY KEY (event_id, role, ordinal),
  FOREIGN KEY (event_id, match_id) REFERENCES match_data.match_events(id, match_id) ON DELETE CASCADE,
  FOREIGN KEY (participant_id, match_id) REFERENCES match_data.match_participants(id, match_id)
);

CREATE TABLE match_data.event_reversals (
  reversal_event_id uuid PRIMARY KEY REFERENCES match_data.match_events(id),
  reversed_event_id uuid NOT NULL UNIQUE REFERENCES match_data.match_events(id),
  reason varchar(500) NOT NULL CHECK (char_length(btrim(reason)) >= 3),
  created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);

CREATE TABLE platform.idempotency_records (
  scope varchar(160) NOT NULL,
  idempotency_key varchar(128) NOT NULL,
  request_hash bytea NOT NULL,
  response_status smallint NOT NULL,
  response_body jsonb NOT NULL,
  created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  expires_at timestamptz NOT NULL,
  PRIMARY KEY (scope, idempotency_key)
);
CREATE INDEX idempotency_expiry_ix ON platform.idempotency_records (expires_at);

CREATE TABLE platform.outbox_events (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  aggregate_type varchar(64) NOT NULL,
  aggregate_id uuid NOT NULL,
  aggregate_sequence integer,
  event_type varchar(128) NOT NULL,
  payload jsonb NOT NULL,
  occurred_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  published_at timestamptz,
  attempts integer NOT NULL DEFAULT 0
);
CREATE INDEX outbox_unpublished_ix ON platform.outbox_events (occurred_at) WHERE published_at IS NULL;

CREATE TABLE read_model.consumer_inbox (
  consumer_name varchar(100) NOT NULL,
  source_event_id uuid NOT NULL,
  processed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
  PRIMARY KEY (consumer_name, source_event_id)
);

CREATE TABLE read_model.match_snapshots (
  match_id uuid PRIMARY KEY,
  organization_id uuid NOT NULL,
  status platform.match_status NOT NULL,
  home_score smallint NOT NULL DEFAULT 0,
  away_score smallint NOT NULL DEFAULT 0,
  last_event_sequence integer NOT NULL DEFAULT 0,
  generated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
