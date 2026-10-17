package auth

import (
	"strings"
	"testing"
	"time"
)

// testIP 文档保留地址(示例用,非真实来源)。
const testIP = "203.0.113.10"

func TestVerifyCaptcha(t *testing.T) {
	id := randomCode(16)
	captchaStore.m[id] = captchaEntry{digest: answerDigest("Ab3d"), bindIP: testIP, expires: time.Now().Add(captchaTTL)}

	// 大小写不敏感 + 首尾空白容忍
	if !VerifyCaptcha(id, " ab3D ", testIP) {
		t.Fatal("正确验证码应通过(大小写不敏感)")
	}
	// 一次性:销毁后同码重放必须失败
	if VerifyCaptcha(id, "Ab3d", testIP) {
		t.Fatal("验证码应一次性,重放须失败")
	}

	// 过期
	id2 := randomCode(16)
	captchaStore.m[id2] = captchaEntry{digest: answerDigest("XY7K"), bindIP: testIP, expires: time.Now().Add(-time.Minute)}
	if VerifyCaptcha(id2, "XY7K", testIP) {
		t.Fatal("过期验证码须失败")
	}

	// 来源绑定:跨来源搬运同一验证码必须失败
	id3 := randomCode(16)
	captchaStore.m[id3] = captchaEntry{digest: answerDigest("Qw2e"), bindIP: testIP, expires: time.Now().Add(captchaTTL)}
	if VerifyCaptcha(id3, "Qw2e", "203.0.113.11") {
		t.Fatal("跨来源提交验证码须失败")
	}
	delete(captchaStore.m, id3)

	// 空入参
	if VerifyCaptcha("", "ABCD", testIP) || VerifyCaptcha("someid", "", "") {
		t.Fatal("空 id 或空码须失败")
	}
}

func TestCaptchaSVG(t *testing.T) {
	svg := renderSVG("A1B2")
	if !strings.HasPrefix(svg, `<svg xmlns="http://www.w3.org/2000/svg"`) {
		t.Fatal("SVG 输出格式不对")
	}
	if strings.Contains(svg, "<text") {
		t.Fatal("SVG 不应包含文本节点(答案会被机械读取)")
	}
	if !strings.Contains(svg, "<path") {
		t.Fatal("字符应以笔画路径渲染")
	}
	if strings.Contains(svg, "A1B2") {
		t.Fatal("SVG 不应包含答案明文")
	}
	if got := randomCode(4); len(got) != 4 {
		t.Fatalf("randomCode 长度=%d,应为 4", len(got))
	}
}
