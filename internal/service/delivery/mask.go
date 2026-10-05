package delivery

import (
	"regexp"
	"strings"
)

// 凭据不可逆掩码(INV-40/INV-42):日志行与失败摘要落库前必须完成掩码;
// 掩码为字面替换,原文不留存、不可逆。宁可过掩,不可漏掩。
const maskMark = "[已掩码]"

var (
	pemBlockRe   = regexp.MustCompile(`-----BEGIN [^-\n]*?-----`)
	credAssignRe = regexp.MustCompile(`(?i)\b(password|passwd|pwd|token|secret|client_secret|authorization|access_key|access-key|user_token)\b(\s*[:=]\s*)([^\s,'"]+)`)
	bearerRe     = regexp.MustCompile(`(?i)\b(bearer)\s+[A-Za-z0-9\-._~+/]{8,}={0,2}`)
	optSecretRe  = regexp.MustCompile(`(?i)(^|\s)((?:--password|--passwd|--token|--secret|-p)\s+)('[^']*'|"[^"]*"|\S+)`)
	jwtRe        = regexp.MustCompile(`\b[A-Za-z0-9._-]{24,}\.[A-Za-z0-9._-]{24,}\.[A-Za-z0-9._-]{10,}\b`)
)

// MaskSecret 对单行文本执行凭据掩码。
// 幂等保证: 各正则命中"已是掩码标记"的值时原样返回,不产生二次替换痕迹。
func MaskSecret(s string) string {
	s = pemBlockRe.ReplaceAllString(s, maskMark)
	s = optSecretRe.ReplaceAllStringFunc(s, func(m string) string {
		g := optSecretRe.FindStringSubmatch(m)
		if strings.TrimSpace(g[3]) == maskMark {
			return m
		}
		return g[1] + g[2] + maskMark
	})
	s = credAssignRe.ReplaceAllStringFunc(s, func(m string) string {
		g := credAssignRe.FindStringSubmatch(m)
		if strings.EqualFold(g[3], "bearer") {
			// 交由 bearerRe 连同后续令牌一并掩码,避免双重标记
			return g[1] + g[2] + g[3]
		}
		if strings.TrimSpace(g[3]) == maskMark {
			return m
		}
		return g[1] + g[2] + maskMark
	})
	s = bearerRe.ReplaceAllStringFunc(s, func(m string) string {
		g := bearerRe.FindStringSubmatch(m)
		if strings.TrimSpace(m[len(g[1]):]) == maskMark {
			return m
		}
		return g[1] + " " + maskMark
	})
	return jwtRe.ReplaceAllString(s, maskMark)
}
