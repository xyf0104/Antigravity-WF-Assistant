<script setup>
import { computed, ref, watch } from "vue";
import Modal from "@/components/ui/Modal.vue";
import Button from "@/components/ui/Button.vue";
import {
	applyCursorProjectMCPConfiguration,
  applyTargetMCPConfiguration,
	chooseCursorProjectMCPConfiguration,
	deleteCursorProjectMCPBackup,
  deleteTargetMCPBackup,
	getCursorProjectMCPConfiguration,
  getTargetMCPConfiguration,
	listCursorProjectMCPBackups,
  listTargetMCPBackups,
	removeCursorProjectMCPConfiguration,
  removeTargetMCPConfiguration,
	restoreCursorProjectMCPBackup,
  restoreTargetMCPBackup,
} from "@/state/appState";

const props = defineProps({
  open: Boolean,
  inline: Boolean,
  target: { type: String, default: "" },
});
const emit = defineEmits(["close", "changed"]);

const data = ref(null);
const mcpScope = ref("global");
const projectSelection = ref({ id: "", name: "", expiresAt: "" });
// This is intentionally component-local. It never enters appState,
// localStorage, logs, diagnostics, or a native response view model.
const remoteURL = ref("");
const loading = ref(false);
const choosingProject = ref(false);
const saving = ref(false);
const removing = ref(false);
const backupsLoading = ref(false);
const backupActionID = ref("");
const backupItems = ref([]);
const backupUnavailable = ref(false);
const backupError = ref("");
const error = ref("");
const notice = ref("");

const isCursor = computed(() => props.target === "cursor");
const isSupportedTarget = computed(() => props.target === "cursor" || props.target === "windsurf");
const displayName = computed(() => isCursor.value ? "Cursor" : "Windsurf");
const isProjectScope = computed(() => isCursor.value && mcpScope.value === "project");
const hasProjectSelection = computed(() => Boolean(projectSelection.value.id));
const selectedProjectName = computed(() => projectSelection.value.name || "所选项目");
const scopeLabel = computed(() => isProjectScope.value ? "项目 MCP" : "全局 MCP");
const snapshot = computed(() => data.value?.snapshot || {});
const busy = computed(() => loading.value || choosingProject.value || saving.value || removing.value || backupsLoading.value || Boolean(backupActionID.value));
// A valid current MCP JSON is insufficient for a write: the same operation
// must also be able to create and verify a recovery point. Until that check
// succeeds, keep the configuration strictly read-only instead of letting the
// native transaction reject an apparently enabled save button.
const recoveryPointsVerified = computed(() => !backupsLoading.value && !backupUnavailable.value && !backupError.value);
const configurationEligible = computed(() => Boolean((!isProjectScope.value || hasProjectSelection.value) && data.value?.canApply && recoveryPointsVerified.value));
const canApply = computed(() => Boolean(configurationEligible.value && remoteURL.value.trim()));
const canRemove = computed(() => Boolean(configurationEligible.value && snapshot.value.managedServerConfigured));
const recoveryPointCount = computed(() => backupItems.value.length);

const clientState = computed(() => isProjectScope.value ? (hasProjectSelection.value ? "已选择" : "未选择") : (data.value?.clientDetected ? "已确认" : "未确认"));
const configState = computed(() => {
  if (isProjectScope.value && !hasProjectSelection.value) return "等待选择";
  if (!snapshot.value.valid) return "需要处理";
  if (snapshot.value.hasSensitiveConfiguration) return "只读保护";
  return snapshot.value.exists ? "结构已验证" : "准备创建";
});
const managedState = computed(() => snapshot.value.managedServerConfigured ? "已配置" : "未配置");
const statusDescription = computed(() => {
  if (isProjectScope.value && !hasProjectSelection.value) return "选择一个 Cursor 项目目录后，XIASS Tools 才会检查并管理该项目的 .cursor/mcp.json。";
  if (!data.value) return "等待本机检查。";
  if (!data.value.ok) return "本机 MCP 设置尚未通过安全检查；当前内容不会被修改。";
  if (backupsLoading.value) return "正在核验可恢复备份；核验完成前不会写入全局 MCP 设置。";
  if (backupUnavailable.value) return "当前安装包无法管理 MCP 恢复点；为保护现有设置，远程配置保持只读。";
  if (backupError.value) return "MCP 恢复点无法安全验证；为保护现有设置，远程配置保持只读。";
  if (!isProjectScope.value && !data.value.clientDetected) return `尚未确认 ${displayName.value}；不会创建或修改全局 MCP 设置。`;
  if (snapshot.value.hasSensitiveConfiguration) return "检测到受保护的 MCP 设置。XIASS Tools 不会读取、展示或改写其中内容。";
  if (!snapshot.value.valid) return "全局 MCP 设置无法安全验证，修复前不会写入。";
  return snapshot.value.managedServerConfigured
    ? "XIASS Tools 的 MCP 远程配置已写入；尚未测试远端 MCP 服务。"
    : "全局 MCP 设置与恢复点已验证，可以添加 XIASS Tools MCP 远程配置。";
});

function clearEndpoint() {
  remoteURL.value = "";
}

function clearProjectSelection() {
  projectSelection.value = { id: "", name: "", expiresAt: "" };
}

function applyProjectSelection(result) {
  if (!result || typeof result !== "object") return false;
  const id = typeof result.selectionId === "string" ? result.selectionId.trim() : "";
  if (!id) return false;
  projectSelection.value = {
    id,
    name: typeof result.projectName === "string" && result.projectName.trim() ? result.projectName.trim() : "所选项目",
    expiresAt: typeof result.expiresAt === "string" ? result.expiresAt : "",
  };
  return true;
}

function activeSelectionID() {
  return isProjectScope.value ? projectSelection.value.id : "";
}

function applyStatus(result) {
  data.value = result && typeof result === "object" ? result : null;
}

function resetBackupView() {
  backupItems.value = [];
  backupUnavailable.value = false;
  backupError.value = "";
  backupActionID.value = "";
}

function safeRecoveryPoint(backup) {
  if (!backup || typeof backup !== "object") return null;
  // Match the native manager's opaque recovery-point ID grammar. This keeps
  // malformed renderer data from reaching a restore/delete bridge call while
  // accepting the time-stamped IDs the verified manager actually emits.
  const id = typeof backup.id === "string" && /^[0-9a-fTZ.-]{32,128}$/.test(backup.id) ? backup.id : "";
  if (!id) return null;
  const createdAt = typeof backup.createdAt === "string" || typeof backup.createdAt === "number" ? backup.createdAt : "";
  return {
    id,
    createdAt,
    // Reasons are generated by the native recovery manager. Map them to
    // product labels rather than rendering an arbitrary raw value.
    reason: backup.reason === "apply" || backup.reason === "remove" || backup.reason === "restore" ? backup.reason : "verified",
    originalExisted: backup.originalExisted === true,
  };
}

function applyBackupList(result) {
  const rawBackups = Array.isArray(result?.backups) ? result.backups : [];
  backupItems.value = rawBackups.map(safeRecoveryPoint).filter(Boolean);
  backupUnavailable.value = result?.unavailable === true;
  const verified = result?.ok === true;
  if (verified || backupUnavailable.value) backupError.value = "";
  return verified;
}

function recoveryReasonLabel(reason) {
  if (reason === "apply") return "保存远程连接前的恢复点";
  if (reason === "remove") return "移除 XIASS 连接前的恢复点";
  if (reason === "restore") return "恢复 MCP 设置前的安全恢复点";
  return "经过校验的恢复点";
}

function formatRecoveryTime(value) {
  const timestamp = new Date(value);
  return Number.isNaN(timestamp.getTime()) ? "创建时间未知" : timestamp.toLocaleString();
}

async function refreshBackups({ quiet = false } = {}) {
  if (!isSupportedTarget.value) return null;
  if (isProjectScope.value && !hasProjectSelection.value) {
    backupItems.value = [];
    backupUnavailable.value = false;
    backupError.value = "";
    return null;
  }
  backupsLoading.value = true;
  if (!quiet) backupError.value = "";
  try {
    const result = isProjectScope.value
      ? await listCursorProjectMCPBackups(activeSelectionID())
      : await listTargetMCPBackups(props.target);
    if (!applyBackupList(result) && !backupUnavailable.value) {
      backupError.value = "无法读取经过校验的 MCP 恢复点；当前全局 MCP 设置未被修改。";
    }
    return result;
  } catch {
    backupItems.value = [];
    backupUnavailable.value = false;
    backupError.value = "无法读取经过校验的 MCP 恢复点；当前全局 MCP 设置未被修改。";
    return null;
  } finally {
    backupsLoading.value = false;
  }
}

async function refresh() {
  if (!isSupportedTarget.value) return;
  if (isProjectScope.value && !hasProjectSelection.value) {
    data.value = null;
    resetBackupView();
    return;
  }
  loading.value = true;
  error.value = "";
  notice.value = "";
  try {
    const [result] = await Promise.all([
      isProjectScope.value
        ? getCursorProjectMCPConfiguration(activeSelectionID())
        : getTargetMCPConfiguration(props.target),
      refreshBackups({ quiet: true }),
    ]);
    applyStatus(result);
    if (isProjectScope.value) applyProjectSelection(result);
    if (!result?.ok) {
      error.value = "无法读取本机 MCP 设置状态；当前设置没有被修改。";
      return;
    }
    notice.value = "已完成本机 MCP 设置与恢复点检查。";
  } catch {
    data.value = null;
    clearEndpoint();
    error.value = "无法读取本机 MCP 设置状态；当前设置没有被修改。";
  } finally {
    loading.value = false;
  }
}

async function refreshAfterMutation() {
  const [result] = await Promise.all([
    isProjectScope.value
      ? getCursorProjectMCPConfiguration(activeSelectionID())
      : getTargetMCPConfiguration(props.target),
    refreshBackups({ quiet: true }),
  ]);
  if (result?.ok) {
    applyStatus(result);
    if (isProjectScope.value) applyProjectSelection(result);
  }
  return result;
}

async function chooseProjectDirectory() {
  if (!isCursor.value || busy.value) return;
  choosingProject.value = true;
  error.value = "";
  notice.value = "";
  clearEndpoint();
  try {
    const result = await chooseCursorProjectMCPConfiguration();
    if (!result?.ok) {
      error.value = "无法选择可安全管理的 Cursor 项目目录；当前 MCP 设置没有被修改。";
      return;
    }
    if (!applyProjectSelection(result)) {
      notice.value = "已取消选择 Cursor 项目目录。";
      return;
    }
    applyStatus(result);
    await refreshBackups({ quiet: true });
    notice.value = `已选择项目“${selectedProjectName.value}”。XIASS Tools 仅管理该项目的 .cursor/mcp.json。`;
  } catch {
    error.value = "无法选择可安全管理的 Cursor 项目目录；当前 MCP 设置没有被修改。";
  } finally {
    choosingProject.value = false;
  }
}

function selectMCPScope(nextScope) {
  if (busy.value || (nextScope !== "global" && nextScope !== "project") || (!isCursor.value && nextScope === "project")) return;
  if (mcpScope.value === nextScope) return;
  mcpScope.value = nextScope;
  clearEndpoint();
  error.value = "";
  notice.value = "";
  resetBackupView();
  if (nextScope === "global" || hasProjectSelection.value) {
    void refresh();
    return;
  }
  data.value = null;
}

async function save() {
  error.value = "";
  notice.value = "";
	if (!configurationEligible.value) {
		error.value = "无法安全验证 MCP 恢复点；当前全局 MCP 设置保持只读，未写入任何远程地址。";
		return;
	}
  if (!canApply.value) {
    error.value = "请先确认客户端已安装、现有 MCP 设置与恢复点均可安全管理，并填写远程地址。";
    return;
  }
  saving.value = true;
  const request = { remoteUrl: remoteURL.value };
  // Clear the rendered field before awaiting native work. The endpoint stays
  // only inside this short-lived request and the native call stack.
  clearEndpoint();
  try {
    const result = isProjectScope.value
      ? await applyCursorProjectMCPConfiguration(activeSelectionID(), request.remoteUrl)
      : await applyTargetMCPConfiguration(props.target, request.remoteUrl);
    request.remoteUrl = "";
    applyStatus(result);
    if (!result?.ok) {
      error.value = "未能安全保存 MCP 远程连接；现有设置已保持不变。";
      return;
    }
    await refreshAfterMutation();
    notice.value = "MCP 远程配置已安全保存，并创建了经过校验的恢复点；尚未测试远端 MCP 服务。";
    emit("changed");
  } catch {
    request.remoteUrl = "";
    error.value = "未能安全保存 MCP 远程连接；现有设置已保持不变。";
  } finally {
    request.remoteUrl = "";
    saving.value = false;
  }
}

async function removeManagedConnection() {
  error.value = "";
  notice.value = "";
	if (!configurationEligible.value) {
		error.value = "无法安全验证 MCP 恢复点；当前全局 MCP 设置保持只读。";
		return;
	}
  if (!canRemove.value) {
    error.value = "请先确认客户端已安装且全局 MCP 设置与恢复点均可安全管理；受保护的设置不会被修改。";
    return;
  }
  // A remote address in the draft is unrelated to removal and must not remain
  // rendered while the native removal request is running.
  clearEndpoint();
  removing.value = true;
  try {
    const result = isProjectScope.value
      ? await removeCursorProjectMCPConfiguration(activeSelectionID())
      : await removeTargetMCPConfiguration(props.target);
    if (!result?.ok) {
      error.value = "未能安全移除 XIASS Tools MCP 远程连接；现有设置已保持不变。";
      return;
    }
    await refreshAfterMutation();
    if (result?.result?.removed === true) {
      notice.value = "已移除 XIASS Tools 的 MCP 远程连接，并创建了经过校验的恢复点；其他 MCP 条目保持不变。";
      emit("changed");
      return;
    }
    notice.value = "未发现 XIASS Tools 的 MCP 远程连接；现有全局 MCP 设置未被修改。";
  } catch {
    error.value = "未能安全移除 XIASS Tools MCP 远程连接；现有设置已保持不变。";
  } finally {
    removing.value = false;
  }
}

function confirmManagedRemoval() {
  if (!canRemove.value) return;
  const location = isProjectScope.value ? `项目“${selectedProjectName.value}”` : `${displayName.value} 全局 MCP 设置`;
  if (!window.confirm(`将只移除 ${location}中名为 xiass-tools 的 XIASS Tools 保留条目；不会删除其他 MCP 条目。操作前会创建一个经过校验的恢复点，是否继续？`)) return;
  void removeManagedConnection();
}

async function runRecoveryAction(action, backupID) {
  error.value = "";
  notice.value = "";
  backupError.value = "";
  backupActionID.value = `${action}:${backupID}`;
  let opaqueID = String(backupID || "");
  try {
    const result = isProjectScope.value
      ? (action === "restore"
        ? await restoreCursorProjectMCPBackup(activeSelectionID(), opaqueID)
        : await deleteCursorProjectMCPBackup(activeSelectionID(), opaqueID))
      : (action === "restore"
        ? await restoreTargetMCPBackup(props.target, opaqueID)
        : await deleteTargetMCPBackup(props.target, opaqueID));
    opaqueID = "";
    if (!result?.ok) {
      backupError.value = action === "restore"
        ? "未能恢复全局 MCP 设置；当前设置已保持不变。"
        : "未能删除 MCP 恢复点；现有设置未被修改。";
      return;
    }
    await refreshAfterMutation();
    notice.value = action === "restore"
      ? "已恢复选定的 MCP 恢复点，并创建新的安全恢复点。"
      : "已删除选定的 MCP 恢复点。";
    emit("changed");
  } catch {
    opaqueID = "";
    backupError.value = action === "restore"
      ? "未能恢复全局 MCP 设置；当前设置已保持不变。"
      : "未能删除 MCP 恢复点；现有设置未被修改。";
  } finally {
    opaqueID = "";
    backupActionID.value = "";
  }
}

function confirmRecoveryRestore(backupID) {
  if (!window.confirm(`将恢复选定的 ${displayName.value} MCP 恢复点。当前全局 MCP 设置会先创建新的安全恢复点；只有这次确认的操作会执行，是否继续？`)) return;
  void runRecoveryAction("restore", backupID);
}

function confirmRecoveryDelete(backupID) {
  if (!window.confirm(`将删除选定的 ${displayName.value} MCP 恢复点。只有这次确认的删除会执行，且无法恢复，是否继续？`)) return;
  void runRecoveryAction("delete", backupID);
}

function close() {
  if (busy.value) return;
  clearEndpoint();
  clearProjectSelection();
  mcpScope.value = "global";
  emit("close");
}

watch(() => [props.open, props.target], ([open, target]) => {
  clearEndpoint();
  error.value = "";
  notice.value = "";
  resetBackupView();
  if (open && target) {
    if (target !== "cursor") mcpScope.value = "global";
    void refresh();
    return;
  }
  if (!open) {
    data.value = null;
    clearProjectSelection();
    mcpScope.value = "global";
  }
});
</script>

<template>
  <Modal :open="open" :title="`配置 ${displayName} MCP`" wide persistent :inline="inline" :closable="!busy" @close="close">
    <div class="mcp-config">
      <p class="intro">
        XIASS Tools 只管理一个保留的远程 MCP 条目，不读取或改写账号、Cookie、令牌、聊天记录、数据库或其他 MCP 条目。
      </p>

      <section v-if="isCursor" class="scope-ribbon" aria-label="Cursor MCP 配置范围">
        <div class="scope-ribbon-head">
          <div>
            <strong>配置范围</strong>
            <span>全局 MCP 面向本机所有 Cursor 工作区；项目 MCP 只作用于你明确选择的一个项目。</span>
          </div>
          <div class="scope-options" role="group" aria-label="选择 MCP 配置范围">
            <button type="button" :class="{ active: mcpScope === 'global' }" :aria-pressed="mcpScope === 'global'" :disabled="busy" @click="selectMCPScope('global')">全局 MCP</button>
            <button type="button" :class="{ active: mcpScope === 'project' }" :aria-pressed="mcpScope === 'project'" :disabled="busy" @click="selectMCPScope('project')">项目 MCP</button>
          </div>
        </div>
        <div v-if="isProjectScope" class="project-handle">
          <div class="project-handle-copy">
            <strong>{{ hasProjectSelection ? selectedProjectName : '尚未选择项目目录' }}</strong>
            <span>{{ hasProjectSelection ? '只会写入该项目的 .cursor/mcp.json；完整本机路径不会显示、保存或导出。' : '从系统目录选择器选择一个真实存在的项目文件夹。' }}</span>
          </div>
          <Button variant="tinted" size="sm" :loading="choosingProject" :disabled="busy && !choosingProject" @click="chooseProjectDirectory">{{ hasProjectSelection ? '更换项目' : '选择项目' }}</Button>
        </div>
      </section>

      <div v-if="loading" class="state-block">正在检查{{ isProjectScope ? '所选项目的' : '本机 ' + displayName + ' 与全局' }} MCP 设置…</div>

      <template v-else>
        <section class="status-card" :class="{ guarded: snapshot.hasSensitiveConfiguration || !snapshot.valid || !configurationEligible }">
          <div>
            <strong>{{ configurationEligible ? `可以安全配置${isProjectScope ? '项目' : ''}` : "暂不可写入" }}</strong>
            <span>{{ statusDescription }}</span>
          </div>
          <span class="status-pill" :class="configurationEligible ? 'ok' : 'warn'">{{ configurationEligible ? "可保存" : "只读保护" }}</span>
        </section>

        <dl class="facts" aria-label="MCP 本机状态">
          <div><dt>{{ isProjectScope ? '项目' : '客户端' }}</dt><dd :class="(isProjectScope ? hasProjectSelection : data?.clientDetected) ? 'ok' : 'muted'">{{ clientState }}</dd></div>
          <div><dt>{{ isProjectScope ? '项目 MCP 设置' : '全局 MCP 设置' }}</dt><dd :class="snapshot.valid && !snapshot.hasSensitiveConfiguration ? 'ok' : 'warn'">{{ configState }}</dd></div>
          <div><dt>XIASS 条目</dt><dd :class="snapshot.managedServerConfigured ? 'ok' : 'muted'">{{ managedState }}</dd></div>
        </dl>

        <p v-if="notice" class="notice" role="status">{{ notice }}</p>
        <p v-if="error" class="error" role="alert">{{ error }}</p>

        <section class="configuration-section" :aria-disabled="!configurationEligible">
          <div class="section-heading">
            <div>
              <strong>{{ isProjectScope ? '项目 MCP 远程地址' : '远程 MCP 地址' }}</strong>
              <span>只支持 HTTPS，或无凭据的本机 localhost/回环 HTTP 地址。保存只会管理名为 xiass-tools 的保留条目。</span>
            </div>
          </div>
          <label class="field">
            <span>远程地址</span>
            <input
              v-model="remoteURL"
              autocomplete="url"
              inputmode="url"
              spellcheck="false"
              placeholder="https://mcp.example.com/endpoint"
              :disabled="!configurationEligible || busy"
            />
            <small>地址只用于这一次保存，不会在工具中心、日志或诊断包中回显。</small>
          </label>
        </section>

        <details class="recovery-points" :aria-label="`经过校验的${isProjectScope ? '项目' : '全局'} MCP 恢复点`">
          <summary>
            <span>经过校验的{{ isProjectScope ? '项目' : '全局' }}恢复点</span>
            <small>{{ backupsLoading ? "正在检查" : backupUnavailable ? "需要更新" : `${recoveryPointCount} 个` }}</small>
          </summary>
          <div class="recovery-panel">
            <p>只列出 XIASS Tools 创建且通过校验的{{ isProjectScope ? '所选项目' : '全局' }}恢复点。列表不显示地址、路径、原始 JSON、请求头、环境变量或认证信息。</p>

            <p v-if="backupsLoading" class="recovery-state" role="status">正在读取经过校验的恢复点…</p>
            <p v-else-if="backupUnavailable" class="recovery-state warning" role="status">当前安装包尚未包含恢复点管理功能。现有 MCP 设置没有被修改。</p>
            <p v-else-if="backupError" class="recovery-state error" role="alert">{{ backupError }}</p>
            <p v-else-if="!recoveryPointCount" class="recovery-state">尚无经过校验的恢复点。</p>

            <div v-else class="recovery-list">
              <article v-for="backup in backupItems" :key="backup.id" class="recovery-row">
                <div class="recovery-meta">
                  <strong>{{ recoveryReasonLabel(backup.reason) }}</strong>
                  <span>{{ formatRecoveryTime(backup.createdAt) }} · 原文件{{ backup.originalExisted ? "存在" : "未创建" }}</span>
                </div>
                <div class="recovery-actions" :aria-label="`${displayName} MCP 恢复点操作`">
                  <Button variant="plain" size="sm" :disabled="busy" :loading="backupActionID === `restore:${backup.id}`" @click="confirmRecoveryRestore(backup.id)">恢复</Button>
                  <Button variant="danger" size="sm" :disabled="busy" :loading="backupActionID === `delete:${backup.id}`" @click="confirmRecoveryDelete(backup.id)">删除</Button>
                </div>
              </article>
            </div>

            <button type="button" class="recovery-refresh" :disabled="busy" @click="refreshBackups">
              <svg :class="{ spin: backupsLoading }" viewBox="0 0 24 24" aria-hidden="true"><path d="M20 11a8 8 0 1 0 2 5.3M20 4v7h-7" /></svg>
              <span>刷新恢复点</span>
            </button>
          </div>
        </details>

        <details class="safety-note">
          <summary>安全边界</summary>
          <p>如果现有{{ isProjectScope ? '项目' : '全局' }} MCP 设置含有环境变量、请求头、认证信息或其他敏感字段，XIASS Tools 会保持它不变。移除连接时也只会删除精确名称为 xiass-tools 的保留条目，不会删除或认领其他 MCP 条目。</p>
        </details>
      </template>
    </div>

    <template #footer>
      <Button variant="plain" :disabled="busy" @click="close">关闭</Button>
      <Button v-if="snapshot.managedServerConfigured" variant="danger" :disabled="!canRemove || busy" :loading="removing" @click="confirmManagedRemoval">移除{{ isProjectScope ? '项目 ' : '' }}XIASS 连接</Button>
      <Button variant="filled" :disabled="!canApply || busy" :loading="saving" @click="save">保存 MCP 连接</Button>
    </template>
  </Modal>
</template>

<style scoped>
.mcp-config { display: grid; width: 100%; min-width: 0; gap: 16px; padding: 2px; overflow-x: clip; overflow-wrap: anywhere; }
.intro { margin: 0; color: var(--text-secondary); font-size: 12px; line-height: 1.65; }
.scope-ribbon { display: grid; gap: 10px; border: 1px solid color-mix(in srgb, var(--accent) 32%, var(--separator)); border-radius: 10px; background: linear-gradient(120deg, color-mix(in srgb, var(--accent) 7%, var(--bg-inset)), color-mix(in srgb, var(--teal) 5%, var(--bg-inset))); padding: 11px 12px; }
.scope-ribbon-head { display: flex; align-items: center; justify-content: space-between; gap: 10px; }.scope-ribbon-head > div:first-child { display: grid; min-width: 0; gap: 2px; }.scope-ribbon-head strong { color: var(--text-primary); font-size: 12px; }.scope-ribbon-head span { color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.scope-options { display: inline-flex; flex: 0 0 auto; gap: 3px; border: 1px solid var(--separator-strong); border-radius: 8px; background: var(--bg-base); padding: 3px; }.scope-options button { min-height: 25px; border: 0; border-radius: 5px; background: transparent; color: var(--text-tertiary); font: inherit; font-size: 10px; font-weight: 700; padding: 0 8px; transition: color .16s var(--ease), background .16s var(--ease), box-shadow .16s var(--ease); }.scope-options button:hover:not(:disabled) { color: var(--text-primary); background: var(--bg-fill-hover); }.scope-options button.active { background: color-mix(in srgb, var(--accent) 16%, var(--bg-fill)); box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--accent) 44%, transparent); color: var(--accent-strong); }.scope-options button:disabled { cursor: wait; opacity: .55; }.scope-options button:focus-visible, .project-handle :deep(.btn:focus-visible) { outline: 2px solid var(--accent-strong); outline-offset: 2px; }
.project-handle { display: flex; align-items: center; justify-content: space-between; gap: 10px; border-top: 1px dashed color-mix(in srgb, var(--accent) 34%, var(--separator)); padding-top: 10px; }.project-handle-copy { display: grid; min-width: 0; gap: 2px; }.project-handle-copy strong { overflow: hidden; color: var(--text-primary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }.project-handle-copy span { color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.state-block { min-height: 112px; display: grid; place-items: center; border: 1px dashed var(--separator-strong); border-radius: 10px; color: var(--text-tertiary); font-size: 12px; }
.status-card { display: flex; align-items: center; justify-content: space-between; gap: 12px; border: 1px solid color-mix(in srgb, var(--green) 42%, var(--separator)); border-left: 3px solid var(--green); border-radius: 10px; background: color-mix(in srgb, var(--green) 6%, var(--bg-inset)); padding: 11px 12px; }
.status-card.guarded { border-color: color-mix(in srgb, var(--orange) 52%, var(--separator)); border-left-color: var(--orange); background: color-mix(in srgb, var(--orange) 7%, var(--bg-inset)); }
.status-card > div { display: grid; min-width: 0; gap: 3px; }
.status-card strong { color: var(--text-primary); font-size: 13px; }
.status-card span { color: var(--text-tertiary); font-size: 11px; line-height: 1.45; }
.status-pill { flex: 0 0 auto; border-radius: 999px; font-size: 10px; font-weight: 720; padding: 4px 7px; }
.status-pill.ok { background: color-mix(in srgb, var(--green) 15%, transparent); color: var(--green); }
.status-pill.warn { background: color-mix(in srgb, var(--orange) 16%, transparent); color: var(--orange); }
.facts { display: grid; grid-template-columns: repeat(3, minmax(0, 1fr)); gap: 8px; margin: 0; }
.facts > div { display: grid; min-width: 0; gap: 3px; border: 1px solid var(--separator); border-radius: 8px; background: var(--bg-inset); padding: 8px 9px; }
.facts dt { color: var(--text-tertiary); font-size: 10px; }
.facts dd { margin: 0; font-family: var(--font-num); font-size: 11px; font-weight: 700; }
.facts dd.ok { color: var(--green); }.facts dd.warn { color: var(--orange); }.facts dd.muted { color: var(--text-tertiary); }
.notice, .error { margin: 0; border-radius: 8px; font-size: 11px; line-height: 1.5; padding: 8px 10px; }
.notice { border: 1px solid color-mix(in srgb, var(--green) 35%, var(--separator)); background: color-mix(in srgb, var(--green) 7%, transparent); color: var(--green); }
.error { border: 1px solid color-mix(in srgb, var(--red) 42%, var(--separator)); background: color-mix(in srgb, var(--red) 8%, transparent); color: var(--red); }
.configuration-section { display: grid; min-width: 0; gap: 12px; border: 1px solid var(--separator); border-radius: 14px; background: var(--bg-inset); padding: 14px 16px 16px; }
.section-heading { display: flex; min-width: 0; justify-content: space-between; gap: 12px; }.section-heading > div { display: grid; min-width: 0; gap: 3px; }
.section-heading strong { color: var(--text-primary); font-size: 12px; }.section-heading span { color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.field { display: grid; min-width: 0; gap: 6px; }.field > span { color: var(--text-secondary); font-size: 11px; }
.field input { width: 100%; min-width: 0; border: 1px solid var(--separator-strong); border-radius: 8px; outline: none; background: var(--bg-base); color: var(--text-primary); font: inherit; font-family: var(--font-num); font-size: 12px; padding: 9px 10px; }
.field input:focus { border-color: var(--accent-strong); box-shadow: 0 0 0 3px var(--accent-soft); }.field input:disabled { cursor: not-allowed; opacity: .58; }
.field small { color: var(--text-tertiary); font-size: 10px; line-height: 1.45; }
.recovery-points { border: 1px solid var(--separator); border-left: 3px solid var(--teal); border-radius: 10px; background: color-mix(in srgb, var(--teal) 5%, var(--bg-inset)); overflow: clip; }
.recovery-points summary { display: flex; min-height: 40px; align-items: center; justify-content: space-between; gap: 10px; cursor: pointer; color: var(--text-primary); font-size: 12px; font-weight: 700; list-style: none; padding: 11px 12px; }
.recovery-points summary::-webkit-details-marker { display: none; }
.recovery-points summary::before { width: 7px; height: 7px; border: 1px solid color-mix(in srgb, var(--teal) 58%, var(--separator)); border-radius: 50%; background: color-mix(in srgb, var(--teal) 24%, transparent); content: ""; }
.recovery-points summary > span { flex: 1 1 auto; }.recovery-points summary small { color: var(--teal); font-size: 10px; font-weight: 700; }
.recovery-points summary:focus-visible { outline: 2px solid var(--accent-strong); outline-offset: -2px; }
.recovery-panel { display: grid; gap: 9px; border-top: 1px solid var(--separator); padding: 10px 11px 11px; }
.recovery-panel > p { margin: 0; color: var(--text-secondary); font-size: 10px; line-height: 1.55; }
.recovery-state { border: 1px dashed var(--separator-strong); border-radius: 8px; color: var(--text-tertiary); font-size: 10px; padding: 9px 10px; }
.recovery-state.warning { border-color: color-mix(in srgb, var(--orange) 42%, var(--separator)); color: var(--orange); }
.recovery-state.error { border-color: color-mix(in srgb, var(--red) 42%, var(--separator)); color: var(--red); }
.recovery-list { display: grid; border-top: 1px solid var(--separator); }
.recovery-row { display: flex; align-items: center; justify-content: space-between; gap: 10px; border-bottom: 1px solid var(--separator); padding: 9px 0; }
.recovery-row:first-child { padding-top: 9px; }.recovery-row:last-child { border-bottom: 0; padding-bottom: 0; }
.recovery-meta { display: grid; min-width: 0; gap: 2px; }.recovery-meta strong { overflow: hidden; color: var(--text-primary); font-size: 11px; text-overflow: ellipsis; white-space: nowrap; }.recovery-meta span { color: var(--text-tertiary); font-family: var(--font-num); font-size: 10px; line-height: 1.4; }
.recovery-actions { display: flex; flex: 0 0 auto; gap: 6px; }.recovery-actions :deep(.btn:focus-visible), .recovery-refresh:focus-visible { outline: 2px solid var(--accent-strong); outline-offset: 2px; }
.recovery-refresh { justify-self: start; display: inline-flex; min-height: 26px; align-items: center; gap: 5px; border: 0; border-radius: 7px; color: var(--text-tertiary); font: inherit; font-size: 10px; padding: 0 6px; transition: color .16s var(--ease), background .16s var(--ease); }
.recovery-refresh svg { width: 13px; height: 13px; fill: none; stroke: currentColor; stroke-width: 1.8; stroke-linecap: round; stroke-linejoin: round; }.recovery-refresh:hover:not(:disabled) { background: var(--bg-fill-hover); color: var(--accent-strong); }.recovery-refresh:disabled { cursor: wait; opacity: .48; }
.safety-note { border-top: 1px solid var(--separator); padding-top: 12px; }.safety-note summary { display: flex; min-height: 40px; align-items: center; cursor: pointer; color: var(--text-primary); font-size: 12px; font-weight: 700; }.safety-note p { margin: 8px 0 0; color: var(--text-tertiary); font-size: 10px; line-height: 1.5; }

/* Embedded workspaces keep their own readable rhythm even at narrow desktop widths. */
.mcp-config { gap: 18px; padding: 4px 8px 20px; }
.intro { font-size: 13px; }
.scope-ribbon-head strong { font-size: 13px; }
.scope-ribbon-head span,
.project-handle-copy span,
.section-heading span,
.field small { font-size: 12px; line-height: 1.55; }
.scope-options { gap: 4px; border-radius: 10px; padding: 4px; }
.scope-options button { min-height: 40px; border-radius: 7px; font-size: 12px; padding-inline: 12px; }
.section-heading strong { font-size: 14px; }
.field > span { font-size: 13px; }
.field input { min-height: 42px; border-radius: 9px; font-size: 13px; padding: 10px 12px; }
@media (max-width: 560px) { .scope-ribbon-head, .project-handle { align-items: stretch; flex-direction: column; }.scope-options { align-self: stretch; }.scope-options button { flex: 1 1 0; }.project-handle :deep(.btn) { justify-content: center; width: 100%; }.facts { grid-template-columns: 1fr; }.recovery-row { align-items: stretch; flex-direction: column; }.recovery-actions :deep(.btn), .recovery-refresh { justify-content: center; width: 100%; } }
</style>
