package analytics

import (
	"encoding/json"
	"errors"
	"io"
	"os"
)

type Config struct {
	TargetServer int32         `json:"target_server"`
	AllianceID   string        `json:"alliance_id"`
	Output       string        `json:"output"`
	Queries      []QueryConfig `json:"queries"`
}

type QueryConfig struct {
	EventID   string `json:"event_id,omitempty"`
	GrabTimes *int32 `json:"grab_times,omitempty"`
	StartRank int32  `json:"start_rank,omitempty"`
	Count     int32  `json:"count,omitempty"`
	PageSize  int32  `json:"page_size,omitempty"`
	SubType   *int32 `json:"sub_type,omitempty"`
	Start     int32  `json:"start,omitempty"`
	End       int32  `json:"end,omitempty"`
	Kind      string `json:"kind"`
	Period    string `json:"period"`
	Day       int32  `json:"day,omitempty"`
	RankType  int32  `json:"rank_type,omitempty"`
	Global    int32  `json:"global,omitempty"`
}

func LoadConfig(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	dec := json.NewDecoder(io.LimitReader(f, 1024*1024))
	dec.DisallowUnknownFields()
	var c Config
	if err := dec.Decode(&c); err != nil {
		return nil, errors.New("invalid analytics configuration JSON")
	}
	if err := dec.Decode(new(any)); !errors.Is(err, io.EOF) {
		return nil, errors.New("trailing analytics configuration data")
	}
	if c.TargetServer <= 0 || c.AllianceID == "" || c.AllianceID == "0" || c.Output == "" || len(c.Queries) == 0 || len(c.Queries) > 32 {
		return nil, errInvalidQuery
	}
	if _, err := c.BuildQueries(); err != nil {
		return nil, err
	}
	return &c, nil
}

const THPRankingType int32 = 13 // Global/EnumType.luac, LWLF 826

func (c *Config) BuildQueries() ([]Query, error) {
	out := make([]Query, 0, len(c.Queries))
	for _, v := range c.Queries {
		if v.Period == "" {
			return nil, errInvalidQuery
		}
		var q Query
		var err error
		switch v.Kind {
		case "vs_daily":
			q, err = VSQuery(0, v.Day, v.Period)
		case "vs_weekly":
			q, err = VSQuery(1, v.Day, v.Period)
		case "vs_season":
			q, err = VSQuery(2, v.Day, v.Period)
		case "membership":
			q, err = RosterQuery(c.AllianceID, v.Period)
		case "meteorite":
			if v.GrabTimes == nil {
				return nil, errInvalidQuery
			}
			start, count := v.StartRank, v.Count
			if start == 0 {
				start = 1
			}
			if count == 0 {
				count = 100
			}
			q, err = MeteoriteQuery(*v.GrabTimes, start, count, v.Period)
		case "svs":
			q = SvSQuery(v.Period)
		case "season_power":
			size := v.PageSize
			if size == 0 {
				size = 100
			}
			q, err = SeasonPowerQuery(v.RankType, size, v.Period)
		case "season_devotes":
			if v.SubType == nil {
				return nil, errInvalidQuery
			}
			start, end := v.Start, v.End
			if start == 0 {
				start = 1
			}
			if end == 0 {
				end = -1
			}
			q, err = SeasonDevotesQuery(v.EventID, *v.SubType, start, end, v.Period)
		case "thp":
			q, err = RankQuery(THPRankingType, v.Global, c.TargetServer, "THP", "heroPower", v.Period)
		default:
			return nil, errors.New("unsupported analytics query kind")
		}
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, nil
}
