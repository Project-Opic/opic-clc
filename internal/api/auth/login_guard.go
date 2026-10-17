// 登录安控 —— 服务端限速与失败退避(进程内滑动窗口计数,无外部依赖)。
//
//   - /auth/captcha 领取按来源 IP 限速;
//   - /auth/login 失败按来源 IP 与账号双维计数,窗口内超过阈值进入冷却期,
//     冷却期内直接拒绝且不消耗验证码;
//   - 验证码类失败只计来源 IP(避免用错误验证码把任意账号打成锁定),
//     凭据类失败同时计来源 IP 与账号。
//
// 边界:计数器为进程内状态,多副本部署各副本独立计数,生产建议在网关再叠加一层
// 按来源限速;ClientIP 依赖引擎的信任代理配置(直接暴露时 X-Forwarded-For 可伪造,
// 须在组合根调用 r.SetTrustedProxies 收敛)。
package auth

import (
	"strings"
	"sync"
	"time"
)

const (
	captchaIPLimit  = 30               // 同源 IP 窗口内验证码领取次数上限
	captchaIPWindow = 5 * time.Minute  // 验证码领取计数窗口
	loginIPLimit    = 20               // 同源 IP 窗口内登录失败上限(验证码错误+凭据错误)
	loginAcctLimit  = 8                // 同账号窗口内凭据失败上限
	loginFailWindow = 15 * time.Minute // 登录失败计数窗口
	loginCooldown   = 10 * time.Minute // 超限后的冷却时长
	guardSweepSize  = 512              // 计数表超过该规模时顺手清理过期项(防内存无界增长)
)

// guardCounter 滑动窗口计数器(含冷却终点)。
type guardCounter struct {
	windowStart time.Time
	count       int
	coolUntil   time.Time
}

var guard = struct {
	sync.Mutex
	ipLogin   map[string]*guardCounter // 登录失败:按来源 IP
	acctLogin map[string]*guardCounter // 凭据失败:按账号
	ipCaptcha map[string]*guardCounter // 验证码领取:按来源 IP
}{
	ipLogin:   map[string]*guardCounter{},
	acctLogin: map[string]*guardCounter{},
	ipCaptcha: map[string]*guardCounter{},
}

// guardBump 记一次计数:窗口过期即重置,计数超过上限即进入冷却。须持锁调用。
func guardBump(m map[string]*guardCounter, key string, limit int, window, cooldown time.Duration, now time.Time) {
	c := m[key]
	if c == nil || now.Sub(c.windowStart) > window {
		c = &guardCounter{windowStart: now}
		m[key] = c
	}
	c.count++
	if c.count > limit {
		c.coolUntil = now.Add(cooldown)
	}
}

// guardCooled 判定是否处于冷却期(只读)。须持锁调用。
func guardCooled(m map[string]*guardCounter, key string, now time.Time) bool {
	c := m[key]
	return c != nil && now.Before(c.coolUntil)
}

// guardSweep 清理窗口过期且无冷却的计数项。须持锁调用。
func guardSweep(m map[string]*guardCounter, now time.Time) {
	if len(m) < guardSweepSize {
		return
	}
	for k, c := range m {
		if now.Sub(c.windowStart) > loginFailWindow && !now.Before(c.coolUntil) {
			delete(m, k)
		}
	}
}

// captchaIssueAllowed 验证码领取按来源限速;超限返回 false(由调用方审计并拒绝)。
func captchaIssueAllowed(ip string) bool {
	guard.Lock()
	defer guard.Unlock()
	now := time.Now()
	guardSweep(guard.ipCaptcha, now)
	if guardCooled(guard.ipCaptcha, ip, now) {
		return false
	}
	guardBump(guard.ipCaptcha, ip, captchaIPLimit, captchaIPWindow, captchaIPWindow, now)
	return !guardCooled(guard.ipCaptcha, ip, now)
}

// loginGuarded 判定登录是否处于失败退避期(来源 IP 或账号任一命中即拒绝)。
func loginGuarded(ip, username string) bool {
	guard.Lock()
	defer guard.Unlock()
	now := time.Now()
	if guardCooled(guard.ipLogin, ip, now) {
		return true
	}
	if u := strings.ToLower(strings.TrimSpace(username)); u != "" && guardCooled(guard.acctLogin, u, now) {
		return true
	}
	return false
}

// loginFail 记一次登录失败:credential=false 只计来源 IP;credential=true 同时计账号。
func loginFail(ip, username string, credential bool) {
	guard.Lock()
	defer guard.Unlock()
	now := time.Now()
	guardSweep(guard.ipLogin, now)
	guardBump(guard.ipLogin, ip, loginIPLimit, loginFailWindow, loginCooldown, now)
	if credential {
		if u := strings.ToLower(strings.TrimSpace(username)); u != "" {
			guardSweep(guard.acctLogin, now)
			guardBump(guard.acctLogin, u, loginAcctLimit, loginFailWindow, loginCooldown, now)
		}
	}
}

// loginOK 登录成功清除该账号的失败计数(IP 维保留,防撞库命中后立即重置来源计数)。
func loginOK(username string) {
	guard.Lock()
	defer guard.Unlock()
	if u := strings.ToLower(strings.TrimSpace(username)); u != "" {
		delete(guard.acctLogin, u)
	}
}
