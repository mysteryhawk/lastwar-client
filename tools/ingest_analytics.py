#!/usr/bin/env python3
"""Transactional, idempotent DoAC JSONL -> SQLite import (Python stdlib only).

Use a separate history database until the existing dashboard schema is inspected.
Snapshots contain private member data. No credentials/raw response dumps accepted.
"""
import argparse
import datetime as dt
import hashlib
import json
import os
import sqlite3
from pathlib import Path

SCHEMA = """
PRAGMA foreign_keys=ON;
CREATE TABLE IF NOT EXISTS analytics_snapshots(
 id TEXT PRIMARY KEY, collected_at TEXT NOT NULL, source_server INTEGER NOT NULL,
 target_server INTEGER NOT NULL, alliance_id TEXT NOT NULL, command TEXT NOT NULL,
 metric TEXT NOT NULL, period TEXT NOT NULL, day INTEGER NOT NULL,
 status TEXT NOT NULL, error_code TEXT, payload_present INTEGER NOT NULL,
 complete_roster INTEGER NOT NULL, validation TEXT NOT NULL, rejected_rows INTEGER NOT NULL, parameters_json TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS analytics_records(
 snapshot_id TEXT NOT NULL REFERENCES analytics_snapshots(id), uid TEXT NOT NULL,
 name TEXT NOT NULL, alliance_id TEXT NOT NULL, server_id INTEGER NOT NULL,
 rank INTEGER, value INTEGER, PRIMARY KEY(snapshot_id,uid));
CREATE INDEX IF NOT EXISTS analytics_history_uid ON analytics_records(uid,snapshot_id);
CREATE INDEX IF NOT EXISTS analytics_history_metric ON analytics_snapshots(metric,period,collected_at);
CREATE TABLE IF NOT EXISTS analytics_membership_events(
 source_server INTEGER NOT NULL, target_server INTEGER NOT NULL, alliance_id TEXT NOT NULL,
 uid TEXT NOT NULL, collected_at TEXT NOT NULL, event TEXT NOT NULL,
 old_name TEXT, new_name TEXT, snapshot_id TEXT NOT NULL REFERENCES analytics_snapshots(id),
 PRIMARY KEY(snapshot_id,uid,event));
CREATE VIEW IF NOT EXISTS analytics_observations AS
 SELECT s.*,r.uid,r.name,r.server_id AS player_server,r.rank,r.value,
 r.alliance_id AS player_alliance FROM analytics_snapshots s
 JOIN analytics_records r ON s.id=r.snapshot_id;
"""

TOP = {'schema_version','collected_at','source_server','target_server','alliance_id',
       'command','metric','period','day','status','error_code','payload_present',
       'complete_roster','validation','rejected_rows','records','parameters'}
ROW = {'uid','name','alliance_id','server_id','rank','value'}
STATUSES = {'ok','empty','partial','missing_payload','server_error','transport_error'}
COMMANDS = {'al.rank','rank.get','al.battle.rank.info','lw.season.rank.info',
            'meteorite.rank.person.info','cross.throne.person.score.rank',
            'lw.season.alliance.devotes.rank.info'}


def integer(value, label, minimum=0, maximum=2**63-1):
    if type(value) is not int or not minimum <= value <= maximum:
        raise ValueError(f'invalid {label}')
    return value


def text(value, label, required=False):
    if not isinstance(value, str) or (required and not value) or len(value) > 4096:
        raise ValueError(f'invalid {label}')
    return value


def validate(s):
    if not isinstance(s, dict) or set(s)-TOP or s.get('schema_version') != 1:
        raise ValueError('unsupported snapshot schema')
    when = dt.datetime.fromisoformat(text(s.get('collected_at'), 'timestamp', True).replace('Z','+00:00'))
    if when.tzinfo is None:
        raise ValueError('timestamp must have timezone')
    s['collected_at'] = when.astimezone(dt.timezone.utc).isoformat(timespec='microseconds')
    for k in ['source_server','target_server','day','rejected_rows']:
        s[k] = integer(s.get(k, 0), k, maximum=2**31-1)
    if s['target_server'] == 0:
        raise ValueError('target server required')
    for k in ['alliance_id','command','metric','period','status']:
        text(s.get(k), k, True)
    if s['command'] not in COMMANDS or s['status'] not in STATUSES:
        raise ValueError('unsupported command/status')
    for k in ['payload_present','complete_roster']:
        if type(s.get(k)) is not bool:
            raise ValueError(f'{k} must be boolean')
    s['validation'] = text(s.get('validation',''), 'validation')
    s['error_code'] = text(s.get('error_code',''), 'error code')
    parameters = s.get('parameters', {})
    allowed = {'type','day','rank_type','global','serverId','event_id','start','end',
               'pageSize','grabTimes','startRank','count'}
    if not isinstance(parameters, dict) or set(parameters)-allowed:
        raise ValueError('unexpected query parameters')
    for key,value in parameters.items():text(value, key)
    s['parameters'] = parameters
    records = s.get('records')
    if not isinstance(records, list):
        raise ValueError('records must be array')
    seen = set()
    for r in records:
        if not isinstance(r, dict) or set(r)-ROW:
            raise ValueError('unexpected record fields')
        uid = text(r.get('uid'), 'uid', True)
        if uid == '0' or uid in seen:
            raise ValueError('invalid/duplicate UID')
        seen.add(uid)
        for k in ['name','alliance_id']:
            r[k] = text(r.get(k,''), k)
        r['server_id'] = integer(r.get('server_id',0), 'player server', maximum=2**31-1)
        for k in ['value','rank']:
            if r.get(k) is not None:
                integer(r[k], k)
        if s['metric'] != 'membership' and r.get('value') is None:
            raise ValueError('metric record requires value')
    if s['status'] in {'server_error','transport_error','missing_payload','empty'} and records:
        raise ValueError('failed/empty snapshot has records')
    if s['complete_roster'] and not (
        s['metric'] == 'membership' and s['command'] == 'al.rank' and s['status'] == 'ok'
        and s['source_server'] == s['target_server'] and s['source_server'] > 0
        and s['payload_present'] and s['rejected_rows'] == 0 and records
        and all(r['alliance_id'] == s['alliance_id'] for r in records)
    ):
        raise ValueError('unsafe authoritative roster')
    return s


def rebuild_membership(db, source, target, alliance):
    """Rebuild in timestamp order, so delayed imports cannot create false events.

    First authoritative roster is a baseline, not proof everyone just joined.
    Incomplete/non-authoritative snapshots never infer departures.
    """
    db.execute('DELETE FROM analytics_membership_events WHERE source_server=? AND target_server=? AND alliance_id=?',
               (source,target,alliance))
    snapshots = db.execute('''SELECT id,collected_at FROM analytics_snapshots
        WHERE source_server=? AND target_server=? AND alliance_id=? AND complete_roster=1
        ORDER BY collected_at,id''', (source,target,alliance)).fetchall()
    previous = None
    for sid, when in snapshots:
        current = dict(db.execute('SELECT uid,name FROM analytics_records WHERE snapshot_id=?', (sid,)))
        if previous is not None:
            for uid in sorted(current.keys()-previous.keys()):
                db.execute('INSERT INTO analytics_membership_events VALUES(?,?,?,?,?,?,?,?,?)',
                           (source,target,alliance,uid,when,'joined',None,current[uid],sid))
            for uid in sorted(previous.keys()-current.keys()):
                db.execute('INSERT INTO analytics_membership_events VALUES(?,?,?,?,?,?,?,?,?)',
                           (source,target,alliance,uid,when,'departed',previous[uid],None,sid))
            for uid in sorted(current.keys() & previous.keys()):
                if current[uid] and previous[uid] and current[uid] != previous[uid]:
                    db.execute('INSERT INTO analytics_membership_events VALUES(?,?,?,?,?,?,?,?,?)',
                               (source,target,alliance,uid,when,'name_changed',previous[uid],current[uid],sid))
        previous = current


def ingest(db, lines):
    added = 0
    affected = set()
    with db:
        for number, line in enumerate(lines, 1):
            if not line.strip():
                continue
            try:
                s = validate(json.loads(line))
            except (ValueError, TypeError, KeyError):
                raise ValueError(f'invalid snapshot at line {number}') from None
            canonical = json.dumps(s,sort_keys=True,separators=(',',':'),ensure_ascii=False)
            sid = hashlib.sha256(canonical.encode()).hexdigest()
            cursor = db.execute('''INSERT OR IGNORE INTO analytics_snapshots VALUES
                (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)''',
                (sid,s['collected_at'],s['source_server'],s['target_server'],s['alliance_id'],
                 s['command'],s['metric'],s['period'],s['day'],s['status'],s['error_code'],
                 s['payload_present'],s['complete_roster'],s['validation'],s['rejected_rows'],
                 json.dumps(s['parameters'],sort_keys=True)))
            if cursor.rowcount == 0:
                continue
            added += 1
            for r in s['records']:
                db.execute('INSERT INTO analytics_records VALUES(?,?,?,?,?,?,?)',
                           (sid,r['uid'],r['name'],r['alliance_id'],r['server_id'],r.get('rank'),r.get('value')))
            if s['complete_roster']:
                affected.add((s['source_server'],s['target_server'],s['alliance_id']))
        for group in sorted(affected):
            rebuild_membership(db,*group)
    return added


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--db',required=True)
    parser.add_argument('jsonl',type=Path)
    args = parser.parse_args()
    # A separate local history database, never the uninspected live dashboard DB.
    fd = os.open(args.db,os.O_CREAT|os.O_RDWR,0o600)
    os.close(fd)
    with sqlite3.connect(args.db) as db:
        db.executescript(SCHEMA)
        with args.jsonl.open() as f:
            print(f'Imported {ingest(db,f)} new snapshots')

if __name__ == '__main__':
    main()
