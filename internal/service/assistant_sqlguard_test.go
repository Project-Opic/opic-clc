package service

import (
	"testing"

	"gitcode.com/opic-ontology/opic-techbase/internal/pkg/ontology"
)

func TestValidateSQLTableRefs(t *testing.T) {
	wl := map[string]ontology.WhitelistEntry{
		"clc_cluster":      {Table: "clc_cluster"},
		"clc_cluster_node": {Table: "clc_cluster_node"},
	}
	allow := []string{
		`SELECT count(*) AS n FROM clc_cluster`,
		`SELECT * FROM clc_cluster a, clc_cluster_node b`,
		`WITH t AS (SELECT 1 AS n) SELECT * FROM t`,
		`SELECT * FROM clc.clc_cluster`,
	}
	for _, sql := range allow {
		if err := validateSQLTableRefs(sql, wl); err != nil {
			t.Errorf("应放行: %s → %v", sql, err)
		}
	}
	deny := []string{
		`SELECT * FROM "sys_user"`,
		`SELECT * FROM clc_cluster, sys_user`,
		`SELECT * FROM clc_cluster/**/, sys_user`,
		`SELECT * FROM (SELECT * FROM sys_user) x`,
		`SELECT * FROM clc_cluster JOIN sys_user u ON u.id = 1`,
	}
	for _, sql := range deny {
		if err := validateSQLTableRefs(sql, wl); err == nil {
			t.Errorf("应拒绝: %s", sql)
		}
	}
}

func TestSQLForbiddenCoversSelectInto(t *testing.T) {
	for _, sql := range []string{
		`SELECT * INTO tmp_x FROM clc_cluster`,
		`SELECT count(*) INTO tmp_x FROM clc_cluster`,
	} {
		if !sqlForbidden.MatchString(sql) {
			t.Errorf("写关键词过滤应命中: %s", sql)
		}
	}
}

func TestSQLLockingCoversForUpdateShare(t *testing.T) {
	for _, sql := range []string{
		`SELECT * FROM clc_cluster FOR UPDATE`,
		`SELECT * FROM clc_cluster FOR SHARE`,
		`SELECT * FROM clc_cluster FOR NO KEY UPDATE`,
		`SELECT * FROM clc_cluster FOR KEY SHARE`,
	} {
		if !sqlLocking.MatchString(sql) {
			t.Errorf("行锁子句过滤应命中: %s", sql)
		}
	}
	if sqlLocking.MatchString(`SELECT * FROM clc_cluster`) {
		t.Error("普通查询不应命中行锁过滤")
	}
}
