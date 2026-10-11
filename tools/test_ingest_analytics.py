import copy
import json
import sqlite3
import unittest
from ingest_analytics import SCHEMA, ingest


def snapshot(when='2026-10-11T00:00:00Z', rows=None, complete=False, metric='membership',status='ok'):
    return dict(schema_version=1,collected_at=when,source_server=1723,target_server=1723,
        alliance_id='synthetic-A',command='al.rank' if metric=='membership' else 'rank.get',
        metric=metric,period='synthetic-period',status=status,payload_present=True,
        complete_roster=complete,rejected_rows=0,validation='synthetic_fixture',records=rows or [])


def row(uid,name='Synthetic'):
    return dict(uid=uid,name=name,alliance_id='synthetic-A')


class IngestTests(unittest.TestCase):
    def setUp(self):
        self.db=sqlite3.connect(':memory:')
        self.db.executescript(SCHEMA)
        self.addCleanup(self.db.close)

    def put(self,*snapshots):
        return ingest(self.db,[json.dumps(s) for s in snapshots])

    def test_idempotent_exact_large_values(self):
        r=row('9007199254740993');r['value']=9223372036854775807
        s=snapshot(rows=[r],metric='THP')
        self.assertEqual(self.put(s),1);self.assertEqual(self.put(s),0)
        self.assertEqual(self.db.execute('SELECT uid,value FROM analytics_records').fetchone(),
                         ('9007199254740993',9223372036854775807))

    def test_failed_line_rolls_back_whole_batch(self):
        s=snapshot(rows=[row('1')]);bad=copy.deepcopy(s);bad['accessToken']='secret'
        with self.assertRaises(ValueError):self.put(s,bad)
        self.assertEqual(self.db.execute('SELECT count(*) FROM analytics_snapshots').fetchone()[0],0)

    def test_no_departures_from_partial_rankings_or_empty_guest(self):
        self.put(snapshot(rows=[row('1'),row('2')],complete=True))
        self.put(snapshot('2026-10-12T00:00:00Z',rows=[row('1')]),
                 snapshot('2026-10-13T00:00:00Z',status='empty'))
        self.assertEqual(self.db.execute('SELECT count(*) FROM analytics_membership_events').fetchone()[0],0)

    def test_authoritative_events_order_independent(self):
        first=snapshot(rows=[row('1','Old'),row('2')],complete=True)
        last=snapshot('2026-10-13T00:00:00Z',rows=[row('1','New'),row('3')],complete=True)
        self.put(last);self.put(first) # late import rebuilds historical transitions
        events=dict(self.db.execute('SELECT uid,event FROM analytics_membership_events'))
        self.assertEqual(events,{'1':'name_changed','2':'departed','3':'joined'})
        self.assertEqual(self.put(first,last),0)

    def test_unsafe_rosters_and_lossy_values_rejected(self):
        for mutate in [lambda s:s.update(source_server=1456),lambda s:s.update(records=[]),
                       lambda s:s.update(status='partial'),lambda s:s.update(rejected_rows=1)]:
            s=snapshot(rows=[row('1')],complete=True);mutate(s)
            with self.assertRaises(ValueError):self.put(s)
        s=snapshot(rows=[dict(row('1'),value=1.5)],metric='THP')
        with self.assertRaises(ValueError):self.put(s)

if __name__=='__main__':unittest.main()
