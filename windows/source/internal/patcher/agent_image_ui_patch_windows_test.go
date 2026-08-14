//go:build windows

package patcher

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func agentImageUIFixture() string {
	return `
const x={createElement:(type,props,...children)=>({type,props:{...(props||{}),children}}),useState:value=>[value,()=>{}],useCallback:value=>value,useContext:()=>({})},On={},rm=(...v)=>v.join(" "),IN=value=>value,Q4a=()=>({open:()=>{},modal:null}),R4a="button",T="icon",tV="card",vV="title",hE=status=>status==="loading",sK=()=>true,ty=()=>({stepHandler:{openFile:()=>{},resolveArtifactUrl:value=>value}}),JU=()=>({blobUrl:void 0}),TD=media=>media.data;
var S4a=({src:a,alt:b,originalFilePath:c,popout:e=!0,className:f="",openUri:g})=>{var [h,k]=(0,x.useState)(!1),l=IN(a),{open:m,modal:n}=Q4a(l,"image"),p=r=>{r&&r.stopPropagation();!h&&g&&c&&(r=c.includes("://")?c:c)&&g(r.toString())};if(!a||h)return x.createElement("span",{className:"text-sm"},"Preview unavailable");a=!(!g||!c);return x.createElement("div",{className:"group/media relative block w-full max-w-3xl"},x.createElement("div",{className:rm("relative overflow-hidden rounded-lg border",e?"cursor-pointer":"",f),onClick:e?m:void 0},x.createElement("img",{src:l,alt:b||"Artifact image",className:"w-full h-auto object-contain",onError:()=>{k(!0)}}),e&&x.createElement(R4a,{onOpen:m,onOpenInTab:a?p:void 0,className:"absolute top-2 right-2"})),b&&x.createElement("div",{className:"text-sm text-secondary-foreground text-center italic"},b),n)};
var mcb=({step:a,status:b})=>{var {stepHandler:{openFile:c,resolveArtifactUrl:e}={}}=ty(),{isRemoteControl:f}=(0,x.useContext)(On),g=a.generatedMedia,{blobUrl:h}=JU(f&&g?.uri?g.uri:void 0),k=void 0;f&&g?.uri?k=h:g?.uri?k=e?.(g.uri)||void 0:g?.payload.case==="inlineData"&&(k=g?TD(g):void 0);b=hE(b)&&!k;f=(0,x.useCallback)(()=>{g?.uri&&c?.(g.uri)},[g?.uri,c]);return k||b?x.createElement("div",{className:"px-2 py-1"},a.prompt&&x.createElement("div",{},a.prompt),b?x.createElement("div",{}):x.createElement("div",{onClick:f},x.createElement("img",{src:k,alt:"Generated image preview",className:"w-full h-auto rounded object-contain"}))):null};
const ncb={"gemini-3.1-flash-image":{displayName:"Gemini 3.1 Flash Image",isNewModel:!0}},tool={generateImage:{isRendered:sK(),icon:a=>x.createElement(T,{name:"image",...a}),isTool:!0,renderer:({step:a,status:b,error:c})=>{var e=!!a.generatedMedia?.uri,f=a.modelName?ncb[a.modelName]:void 0,g=f?.displayName||"Gemini";f=f?.isNewModel??!1;e=hE(b)?` + "`Generating with ${g} \\ud83c\\udf4c`" + `:e?` + "`Generated with ${g} \\ud83c\\udf4c`" + `:` + "`Generate with ${g} \\ud83c\\udf4c`" + `;return x.createElement(tV,{loading:hE(b),title:x.createElement(vV,{prefix:f?x.createElement("span",{},"New"):void 0,content:e}),supplementaryView:c?null:x.createElement(mcb,{step:a,status:b}),cta:null})}}};
`
}

func TestPatchAgentImageUIRuntime(t *testing.T) {
	assertAgentImageUIRuntime(t, agentImageUIFixture())
}

func TestPatchAgentImageUI231Runtime(t *testing.T) {
	source := strings.ReplaceAll(agentImageUIFixture(), "hE", "kK")
	start := strings.Index(source, "var S4a=")
	end := strings.Index(source, "\nvar mcb=")
	if start < 0 || end < 0 || end <= start {
		t.Fatal("base Agent fixture declarations were not found")
	}
	legacyArtifact := `var S4a=({src:a,alt:b,originalFilePath:c,popout:e=!0,className:f="",openUri:g})=>{var [h,k]=(0,x.useState)(!1),l=IN(a),m=()=>{if(!h&&g&&c){let n=c.includes("://")?c:c;g(n.toString())}};return!a||h?x.createElement("span",{className:"text-sm"},"Preview unavailable"):x.createElement("div",{className:"group relative block w-full max-w-3xl"},x.createElement("div",{className:rm("relative overflow-hidden rounded border",e?"cursor-pointer":"",f),onClick:e&&g?m:void 0},x.createElement("img",{src:l,alt:b||"Artifact image",className:"w-full h-auto object-contain",onError:()=>{k(!0)}})),b&&x.createElement("div",{className:"text-sm text-secondary-foreground text-center italic"},b))};`
	source = source[:start] + legacyArtifact + source[end:]
	assertAgentImageUIRuntime(t, source)
}

func TestPatchAgentImageUI2010Runtime(t *testing.T) {
	source := strings.Replace(agentImageUIFixture(), `e=hE(b)?`, `e=[8,9,1,2,11].includes(b)?`, 1)
	assertAgentImageUIRuntimeWithLoadingStatus(t, source, "8")
}

func assertAgentImageUIRuntime(t *testing.T, fixture string) {
	assertAgentImageUIRuntimeWithLoadingStatus(t, fixture, `"loading"`)
}

func assertAgentImageUIRuntimeWithLoadingStatus(t *testing.T, fixture, loadingStatus string) {
	t.Helper()
	updated, result := patchAgentImageUI(fixture)
	if !result.Recognized || !result.Changed {
		t.Fatalf("Agent image UI fixture was not patched: %+v", result)
	}
	for _, marker := range []string{agentImageGenerationUIPatchMarker, agentImageGenerationDedupePatchMarker, imageGenerationUIPatchMarker} {
		if !strings.Contains(updated, marker) {
			t.Fatalf("Agent image UI fixture is missing %s", marker)
		}
	}
	if !strings.Contains(updated, imageArtifactThumbnailStyle) {
		t.Fatal("Agent artifact renderer is missing the 320px thumbnail constraint")
	}
	second, secondResult := patchAgentImageUI(updated)
	if !secondResult.Recognized || secondResult.Changed || second != updated {
		t.Fatalf("Agent image UI patch is not idempotent: %+v", secondResult)
	}

	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is unavailable; Agent image UI runtime check skipped")
	}
	path := filepath.Join(t.TempDir(), "agent-image-ui.js")
	script := `"use strict";` + updated + `
let wfNow=Date.now();Date.now=()=>wfNow;
const title=(modelName,status="done",hasMedia=true)=>tool.generateImage.renderer({step:{modelName,generatedMedia:hasMedia?{uri:"file:///C:/Temp/generated.png"}:void 0},status}).props.title.props.content;
mcb({step:{generatedMedia:{uri:"file:///C:/Temp/generated.png"}},status:"done"});
wfNow+=300000;
const matching=S4a({src:"vscode-file://vscode-app/C:/Temp/generated.png",alt:"duplicate",originalFilePath:"C:\\Temp\\generated.png"});
const different=S4a({src:"file:///C:/Temp/normal.png",alt:"normal",originalFilePath:"C:\\Temp\\normal.png"});
$wfRememberGeneratedImageURI("file:///C:/Temp/generated-two.png");
const renamed=S4a({src:"file:///C:/Temp/artifacts/saved-under-a-different-name.png",alt:"renamed",originalFilePath:"C:\\Temp\\artifacts\\saved-under-a-different-name.png"});
const afterRenamed=S4a({src:"file:///C:/Temp/normal-after.png",alt:"normal after",originalFilePath:"C:\\Temp\\normal-after.png"});
process.stdout.write(JSON.stringify({gpt:title("gpt-image-2"),gemini:title("gemini-3.1-flash-image"),unknown:title("image-alpha"),loading:title(void 0,` + loadingStatus + `,false),matching,differentAlt:different?.props?.children?.[1]?.props?.children?.[0],renamed,afterRenamedAlt:afterRenamed?.props?.children?.[1]?.props?.children?.[0]}));`
	if err := os.WriteFile(path, []byte(script), 0o600); err != nil {
		t.Fatal(err)
	}
	if output, err := exec.Command(node, "--check", path).CombinedOutput(); err != nil {
		t.Fatalf("patched Agent image UI failed node --check: %s: %v", output, err)
	}
	output, err := exec.Command(node, path).CombinedOutput()
	if err != nil {
		t.Fatalf("patched Agent image UI runtime failed: %s: %v", output, err)
	}
	var got struct {
		GPT          string `json:"gpt"`
		Gemini       string `json:"gemini"`
		Unknown      string `json:"unknown"`
		Loading      string `json:"loading"`
		Matching     any    `json:"matching"`
		DifferentAlt string `json:"differentAlt"`
		Renamed      any    `json:"renamed"`
		AfterRenamed string `json:"afterRenamedAlt"`
	}
	if err := json.Unmarshal(output, &got); err != nil {
		t.Fatalf("patched Agent image UI returned invalid JSON %q: %v", output, err)
	}
	if got.GPT != "Generated with GPT Image 2" || got.Gemini != "Generated with Gemini 3.1 Flash Image \U0001F34C" ||
		got.Unknown != "Generated with image-alpha" || got.Loading != "Generating image" || got.Matching != nil || got.DifferentAlt != "normal" ||
		got.Renamed != nil || got.AfterRenamed != "normal after" {
		t.Fatalf("unexpected patched Agent image UI state: %+v", got)
	}
}

func TestPatchAgentImageUIMigratesLegacyDedupe(t *testing.T) {
	current, result := patchAgentImageUI(agentImageUIFixture())
	if !result.Changed {
		t.Fatalf("failed to construct current Agent fixture: %+v", result)
	}
	legacy := strings.Replace(current, agentImageGenerationDedupePatchMarker, agentImageGenerationDedupePatchV1Marker, 1)
	legacy = strings.Replace(legacy, agentGeneratedImageDedupeRegistry(), agentGeneratedImageDedupeV1Registry(), 1)
	legacy = strings.Replace(legacy, imageArtifactThumbnailStyle, "", 1)
	if legacy == current {
		t.Fatal("failed to construct legacy Agent dedupe fixture")
	}
	updated, migration := patchAgentImageUI(legacy)
	if !migration.Recognized || !migration.Changed || !strings.Contains(updated, agentImageGenerationDedupePatchMarker) ||
		strings.Contains(updated, agentImageGenerationDedupePatchV1Marker) || !strings.Contains(updated, `$wfGeneratedImageEvents`) {
		t.Fatalf("legacy Agent dedupe was not migrated: %+v", migration)
	}
	second, secondResult := patchAgentImageUI(updated)
	if !secondResult.Recognized || secondResult.Changed || second != updated {
		t.Fatalf("migrated Agent dedupe is not idempotent: %+v", secondResult)
	}
}

func TestPatchAgentImageUIMigratesV2DedupeWindow(t *testing.T) {
	current, result := patchAgentImageUI(agentImageUIFixture())
	if !result.Changed {
		t.Fatalf("failed to construct current Agent fixture: %+v", result)
	}
	legacy := strings.Replace(current, agentImageGenerationDedupePatchMarker, agentImageGenerationDedupePatchV2Marker, 1)
	legacy = strings.Replace(legacy, agentGeneratedImageDedupeRegistry(), agentGeneratedImageDedupeV2Registry(), 1)
	legacy = strings.Replace(legacy, imageArtifactThumbnailStyle, "", 1)
	if legacy == current {
		t.Fatal("failed to construct v2 Agent dedupe fixture")
	}
	updated, migration := patchAgentImageUI(legacy)
	if !migration.Recognized || !migration.Changed || !strings.Contains(updated, agentImageGenerationDedupePatchMarker) ||
		strings.Contains(updated, agentImageGenerationDedupePatchV2Marker) || strings.Contains(updated, agentGeneratedImageDedupeV2Registry()) {
		t.Fatalf("v2 Agent dedupe was not migrated: %+v", migration)
	}
	if !strings.Contains(updated, `now-$wfGeneratedImageEvents[index].time>6E5`) {
		t.Fatal("migrated Agent dedupe did not receive the ten-minute event window")
	}
}

func TestPatchAgentImageUIMigratesV3ArtifactToThumbnail(t *testing.T) {
	current, result := patchAgentImageUI(agentImageUIFixture())
	if !result.Changed {
		t.Fatalf("failed to construct current Agent fixture: %+v", result)
	}
	legacy := strings.Replace(current, agentImageGenerationDedupePatchMarker, agentImageGenerationDedupePatchV3Marker, 1)
	legacy = strings.Replace(legacy, imageArtifactThumbnailStyle, "", 1)
	if legacy == current {
		t.Fatal("failed to construct v3 Agent dedupe fixture")
	}
	updated, migration := patchAgentImageUI(legacy)
	if !migration.Recognized || !migration.Changed || !strings.Contains(updated, agentImageGenerationDedupePatchMarker) ||
		strings.Contains(updated, agentImageGenerationDedupePatchV3Marker) || !strings.Contains(updated, imageArtifactThumbnailStyle) {
		t.Fatalf("v3 Agent artifact was not migrated to a 320px thumbnail: %+v", migration)
	}
}
