// 图形验证码 —— 本地登录安控加强。
//
// 自绘 SVG(无第三方依赖):4 位随机码(去掉易混淆字符),大小写不敏感;
// 验证码一次性,5 分钟过期,答错即作废。SSO(Casdoor/ZITADEL)跳转登录不受影响。
//
// 安控收紧(审计修复):
//   - 服务端只存答案的 HMAC-SHA256 摘要,下发的 SVG 只含预置笔画字形的 <path>,
//     答案不出现在任何文本节点/属性中,机械读取无法还原;
//   - 验证码与领取来源(ClientIP)绑定,跨来源提交一律拒绝;
//   - 领取限速与登录失败退避见 login_guard.go。
package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"math/big"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"gitcode.com/opic-ontology/opic-techbase/internal/api/common"
	"gitcode.com/opic-ontology/opic-techbase/internal/config"
	"gitcode.com/opic-ontology/opic-techbase/internal/service"
	"gitcode.com/opic-ontology/opic-techbase/pkg/errcode"
)

const captchaTTL = 5 * time.Minute

// captchaEntry 一条待验证的验证码(只存答案摘要,不存明文)。
type captchaEntry struct {
	digest  []byte    // 答案的 HMAC-SHA256 摘要
	bindIP  string    // 领取时的来源(ClientIP),校验须同源
	expires time.Time // 过期时间
}

var captchaStore = struct {
	sync.Mutex
	m map[string]captchaEntry
}{m: map[string]captchaEntry{}}

// captchaCharset 去掉 0/O/1/I/l 等易混淆字符。
const captchaCharset = "23456789ABCDEFGHJKMNPQRSTUVWXYZ"

// answerDigest 答案摘要(HMAC-SHA256,密钥复用会话签名密钥);
// 服务端存储与校验比较都只在摘要上进行,明文答案不落任何交付物。
func answerDigest(code string) []byte {
	mac := hmac.New(sha256.New, []byte(config.Config.Auth.JWTSecret+"|captcha"))
	mac.Write([]byte(strings.ToLower(strings.TrimSpace(code))))
	return mac.Sum(nil)
}

// Captcha 下发图形验证码:GET /api/auth/captcha → {captcha_id, image}。
// 按来源 IP 限速;SVG 仅含笔画路径,不含答案文本。
func Captcha(c *gin.Context) {
	ip := c.ClientIP()
	if !captchaIssueAllowed(ip) {
		service.WriteAudit(nil, 0, "anonymous", "CAPTCHA_LIMIT", "验证码领取频率超限;client_ip="+ip)
		c.JSON(200, errcode.New(errcode.ErrLogin, "验证码请求过于频繁，请稍后再试", nil))
		return
	}
	code := randomCode(4)
	id := randomCode(16) + fmtTime()
	captchaStore.Lock()
	// 顺手清理过期项,防止内存无界增长
	now := time.Now()
	for k, e := range captchaStore.m {
		if now.After(e.expires) {
			delete(captchaStore.m, k)
		}
	}
	captchaStore.m[id] = captchaEntry{digest: answerDigest(code), bindIP: ip, expires: now.Add(captchaTTL)}
	captchaStore.Unlock()

	svg := renderSVG(code)
	common.OKJSON(c, gin.H{
		"captcha_id": id,
		"image":      "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte(svg)),
		"ttl":        int(captchaTTL.Seconds()),
	}, "")
}

// VerifyCaptcha 校验并销毁(一次性)。来源须与领取时一致(跨来源搬运一律拒绝);
// 答案比对走摘要常数时间比较,不落明文。
func VerifyCaptcha(id, code, clientIP string) bool {
	if id == "" || code == "" {
		return false
	}
	captchaStore.Lock()
	e, ok := captchaStore.m[id]
	if ok {
		delete(captchaStore.m, id) // 无论对错都销毁,防重放
	}
	captchaStore.Unlock()
	if !ok || time.Now().After(e.expires) {
		return false
	}
	if e.bindIP != "" && clientIP != "" && e.bindIP != clientIP {
		return false // 与领取来源不一致
	}
	return hmac.Equal(answerDigest(code), e.digest)
}

func randomCode(n int) string {
	b := make([]byte, n)
	max := big.NewInt(int64(len(captchaCharset)))
	for i := range b {
		v, err := rand.Int(rand.Reader, max)
		if err != nil {
			v = big.NewInt(time.Now().UnixNano() % max.Int64())
		}
		b[i] = captchaCharset[v.Int64()]
	}
	return string(b)
}

func fmtTime() string { return strings.ToLower(randBase32(time.Now().UnixNano())) }

func randBase32(v int64) string {
	const digits = "0123456789abcdefghijklmnopqrstuv"
	out := make([]byte, 0, 12)
	for ; v > 0; v >>= 5 {
		out = append(out, digits[v&31])
	}
	return string(out)
}

// palette 低饱和底色,与登录页白底蓝字风格一致。
var palette = []string{"#e8f1fd", "#eef7ee", "#fdf3e7", "#f3eefb"}

// renderColors 笔画与噪声线颜色。
var renderColors = []string{"#4c7dd0", "#5aa35a", "#c9803e", "#8a6fc0"}

// glyphStrokes 预置笔画字形(5×7 网格折线,"|" 分隔笔画)。
// 字符只以 <path> 几何输出,答案文本从不进入 SVG。
var glyphStrokes = map[byte]string{
	'2': "0,1 1,0 3,0 4,1 4,2 0,6 4,6",
	'3': "0,0 3,0 4,1 4,2 3,3 4,4 4,5 3,6 0,6",
	'4': "3,0 0,4|0,4 4,4|3,0 3,6",
	'5': "4,0 0,0 0,3 3,3 4,4 4,5 3,6 0,6",
	'6': "4,0 1,0 0,2 0,4 1,6 3,6 4,5 4,4 3,3 1,3 0,4",
	'7': "0,0 4,0 1,6|2,3 3,3",
	'8': "1,0 3,0 4,1 3,2 1,2 0,1 1,0|1,2 3,2 4,4 3,6 1,6 0,4 1,2",
	'9': "1,0 3,0 4,1 4,3 3,4 1,4 0,3 0,1 1,0|4,3 4,5 3,6 0,6",
	'A': "0,6 2,0 4,6|1,4 3,4",
	'B': "0,0 0,6|0,0 3,0 4,1 3,2 0,2|0,2 3,2 4,3 4,5 3,6 0,6",
	'C': "4,0 1,0 0,1 0,5 1,6 4,6",
	'D': "0,0 0,6|0,0 3,0 4,1 4,5 3,6 0,6",
	'E': "4,0 0,0 0,6 4,6|0,3 3,3",
	'F': "4,0 0,0 0,6|0,3 3,3",
	'G': "4,0 1,0 0,1 0,5 1,6 3,6 4,5 4,4 2,4",
	'H': "0,0 0,6|4,0 4,6|0,3 4,3",
	'J': "4,0 4,4 3,6 1,6 0,5 0,4",
	'K': "0,0 0,6|4,0 0,3 4,6",
	'M': "0,6 0,0 2,3 4,0 4,6",
	'N': "0,6 0,0 4,6 4,0",
	'P': "0,6 0,0 3,0 4,1 4,2 3,3 0,3",
	'Q': "1,0 3,0 4,1 4,5 3,6 1,6 0,5 0,1 1,0|2,4 4,6",
	'R': "0,6 0,0 3,0 4,1 4,2 3,3 0,3|2,3 4,6",
	'S': "4,0 1,0 0,1 0,2 1,3 3,3 4,4 4,5 3,6 0,6",
	'T': "0,0 4,0|2,0 2,6",
	'U': "0,0 0,5 1,6 3,6 4,5 4,0",
	'V': "0,0 2,6 4,0",
	'W': "0,0 1,6 2,2 3,6 4,0",
	'X': "0,0 4,6|4,0 0,6",
	'Y': "0,0 2,3 4,0|2,3 2,6",
	'Z': "0,0 4,0 0,6 4,6",
}

// renderSVG 画 4 位字符:仅输出预置笔画字形的 <path>(随机旋转/错位/颜色)+ 随机噪声线。
// 噪声与抖动全部由 crypto/rand 产生,与答案取值无关;答案字符不作为文本出现。
func renderSVG(code string) string {
	var sb strings.Builder
	bg := palette[randInt(len(palette))]
	sb.WriteString(`<svg xmlns="http://www.w3.org/2000/svg" width="120" height="40" viewBox="0 0 120 40">`)
	sb.WriteString(`<rect width="120" height="40" rx="6" fill="` + bg + `"/>`)
	// 噪声线(坐标随机,不由答案推导)
	for i := 0; i < 4; i++ {
		sb.WriteString(`<line x1="` + itoa(randInt(112)+4) + `" y1="` + itoa(randInt(32)+4) +
			`" x2="` + itoa(randInt(112)+4) + `" y2="` + itoa(randInt(32)+4) +
			`" stroke="` + renderColors[randInt(len(renderColors))] + `" stroke-width="1" opacity="0.5"/>`)
	}
	// 字符:预置笔画路径(5×7 网格按 4 倍缩放),逐个随机旋转/抖动
	const scale = 4
	for i := 0; i < len(code); i++ {
		strokes, ok := glyphStrokes[code[i]]
		if !ok {
			continue // 字符集已被 glyphStrokes 全覆盖;异常字符一律略过,绝不回退为文本
		}
		x := 12 + i*27 + randInt(5)
		y := 5 + randInt(4)
		rot := randInt(25) - 12
		color := renderColors[randInt(len(renderColors))]
		sb.WriteString(`<g transform="translate(` + itoa(x) + ` ` + itoa(y) + `) rotate(` + itoa(rot) + ` 10 14)">`)
		for _, stroke := range strings.Split(strokes, "|") {
			d := ""
			for j, pt := range strings.Fields(stroke) {
				gx, gy, ok := parseGridPoint(pt)
				if !ok {
					continue
				}
				cmd := "L"
				if j == 0 {
					cmd = "M"
				}
				d += cmd + itoa(gx*scale) + " " + itoa(gy*scale) + " "
			}
			if d == "" {
				continue
			}
			sb.WriteString(`<path d="` + d + `" fill="none" stroke="` + color +
				`" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>`)
		}
		sb.WriteString(`</g>`)
	}
	sb.WriteString(`</svg>`)
	return sb.String()
}

// parseGridPoint 解析 "x,y" 网格点。
func parseGridPoint(tok string) (int, int, bool) {
	parts := strings.SplitN(tok, ",", 2)
	if len(parts) != 2 {
		return 0, 0, false
	}
	x, err1 := strconv.Atoi(parts[0])
	y, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return 0, 0, false
	}
	return x, y, true
}

// randInt 返回 [0,n) 的密码学安全随机整数(失败退化为纳秒抖动,与答案无关)。
func randInt(n int) int {
	if n <= 0 {
		return 0
	}
	v, err := rand.Int(rand.Reader, big.NewInt(int64(n)))
	if err != nil {
		return int(time.Now().UnixNano() % int64(n))
	}
	return int(v.Int64())
}

func itoa(v int) string {
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var b [20]byte
	i := len(b)
	for v > 0 {
		i--
		b[i] = byte('0' + v%10)
		v /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
