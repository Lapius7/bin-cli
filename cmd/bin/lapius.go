package main

import "os/exec"

// lapiusFooter は --help / --version の最後に出す作者表示と lapacks の案内。
func lapiusFooter() string {
	s := T("作者: Lapius (https://github.com/Lapius7)") + "\n"
	if _, err := exec.LookPath("lapacks"); err == nil {
		return s + T("@lapius のツール: lapacks で一覧・インストール・更新") + "\n"
	}
	return s + T("@lapius のツール: npm i -g @lapius/lapacks で一覧・インストール・更新を管理") + "\n"
}
