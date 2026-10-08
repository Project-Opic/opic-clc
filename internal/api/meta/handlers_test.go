// handlers_test.go —— /api/meta/dictionaries 的覆盖面与文案真实性(DEF-09 的回归锁)。
//
// 这层是界面唯一允许的字典出口:M1 声明的字典若不在这里下发,前端就只能各抄一份本地副本 ——
// 副本不会报错,只会在字典改动后静默漂移(同族缺陷在姊妹仓已实测发生一次:界面把「集群配置」
// 写死成二值判断,而模型的轴名是「主机清单(hosts)」)。
package meta

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"gitcode.com/opic-ontology/opic-techbase/internal/config"
	"gitcode.com/opic-ontology/opic-techbase/internal/pkg/ontology"
)

// 刻意不下发的字典:下发它只会邀请前端再抄一张第二份目录。
var notExposedByDesign = map[string]string{
	"DICT-CLC_INSTALL_STEP": "装机步骤目录随任务行逐行下发(step_name,REP-12 c1),界面没有也不该有副本",
}

type dictItem struct {
	Code  string `json:"code"`
	Label string `json:"label"`
}

func servedDictionaries(t *testing.T) (map[string][]dictItem, *ontology.Registry) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	config.Config.Ontology.ModelsDir = filepath.Join("..", "..", "..", "models")
	reg := ontology.Load()
	if len(reg.DataDictionaries) == 0 {
		t.Fatalf("本体字典未加载(检查 models_dir=%s 与 manifest.json)", config.Config.Ontology.ModelsDir)
	}
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	Dictionaries(c)
	if w.Code != http.StatusOK {
		t.Fatalf("字典接口应回 200,实际 %d: %s", w.Code, w.Body.String())
	}
	var resp struct {
		ReturnInfo struct {
			ReturnCode string `json:"returnCode"`
		} `json:"returnInfo"`
		Data map[string][]dictItem `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("响应不是可解析的统一信封: %v (%s)", err, w.Body.String())
	}
	if resp.Data == nil {
		t.Fatalf("data 缺失,界面将无字典可取: %s", w.Body.String())
	}
	return resp.Data, reg
}

// 模型侧每声明一个字典,接口侧就必须有一个出口 —— 少一个就是邀请前端抄一个。
func TestDictionariesCoverEveryModelDictionary(t *testing.T) {
	served, reg := servedDictionaries(t)
	var missing []string
	for id, dic := range reg.DataDictionaries {
		if reason, ok := notExposedByDesign[id]; ok {
			if len(served[strings.TrimPrefix(id, "DICT-")]) > 0 {
				t.Errorf("%s 按白名单不该下发(%s),实际下发了", id, reason)
			}
			continue
		}
		name, _ := dic["name"].(string)
		if name == "" {
			name = strings.TrimPrefix(id, "DICT-")
		}
		if len(served[name]) == 0 {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Fatalf("模型声明但接口未下发的字典 %d 个: %v —— 补进 handlers.go,别让前端抄副本", len(missing), missing)
	}
	// 双向核对:既不能漏下发,也不能凭空下发模型里没有的键(那说明字典在代码里硬编码了)。
	if got, want := len(served), len(reg.DataDictionaries)-len(notExposedByDesign); got != want {
		t.Fatalf("下发键数 %d ≠ 模型字典数 %d - 白名单 %d", got, len(reg.DataDictionaries), len(notExposedByDesign))
	}
}

// 下发的必须是中文名而不是码值回吐:label 空或与 code 同串 ⇒ 对端只能显示英文码。
func TestDictionariesServeChineseLabels(t *testing.T) {
	served, _ := servedDictionaries(t)
	for dictName, items := range served {
		for _, it := range items {
			if strings.TrimSpace(it.Code) == "" {
				t.Errorf("%s 存在空码值项", dictName)
			}
			if strings.TrimSpace(it.Label) == "" {
				t.Errorf("%s/%s 的 label 为空,界面只能回吐码值", dictName, it.Code)
			}
			if it.Label == it.Code {
				t.Errorf("%s/%s 的 label 与 code 同串,未取到模型里的中文名", dictName, it.Code)
			}
		}
	}
}

// v1.2 增补的五个字典(向导台 + 模板台界面文案的出处)必须逐键在位。
func TestDictionariesServeV12Additions(t *testing.T) {
	served, _ := servedDictionaries(t)
	want := map[string][]string{
		"CLC_STEP_MODE":      {"STEP", "AUTO"},
		"CLC_STEP_STATUS":    {"PENDING", "RUNNING", "SUCCEEDED", "FAILED", "CANCELLED", "SKIPPED"},
		"CLC_STEP_VERDICT":   {"EXIT_CODE", "PLAY_RECAP", "REMOTE_RECEIPT"},
		"CLC_LOG_LEVEL":      {"INFO", "WARN", "ERROR"},
		"CLC_TEMPLATE_STATE": {"DRAFT", "ENABLED", "DISABLED"},
	}
	for dict, codes := range want {
		got := map[string]string{}
		for _, it := range served[dict] {
			got[it.Code] = it.Label
		}
		for _, code := range codes {
			if got[code] == "" {
				t.Errorf("%s/%s 未下发或无中文名,实际: %v", dict, code, got)
			}
		}
	}
	// 漂移实证位:模型把 STEP 档叫「向导单步」、AUTO 档叫「自动连跑」,
	// 前端曾自创「单步确认 / 连跑」——本断言钉住出处在 M1 而不是界面。
	if l := served["CLC_STEP_MODE"]; !(len(l) == 2 && l[0].Label == "向导单步" && l[1].Label == "自动连跑") {
		t.Errorf("CLC_STEP_MODE 文案应与 M1 一致,实际 %+v", served["CLC_STEP_MODE"])
	}
}
