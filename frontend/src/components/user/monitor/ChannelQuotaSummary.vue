<template>
  <section class="quota-summary">
    <header class="quota-summary-header">
      <div class="min-w-0">
        <h2>{{ title }}</h2>
        <p v-if="dashboard">{{ formattedGeneratedAt }}</p>
      </div>
      <UiIconButton :label="t('common.refresh')" :disabled="loading" @click="emit('refresh')">
        <Icon name="refresh" size="sm" :class="loading ? 'animate-spin' : ''" />
      </UiIconButton>
    </header>

    <div class="quota-metrics">
      <div v-for="metric in metrics" :key="metric.key" class="quota-metric">
        <span>{{ metric.label }}</span>
        <strong :class="metric.tone">{{ metric.value }}</strong>
      </div>
    </div>

    <div v-if="loading && !dashboard" class="quota-state">
      <Icon name="refresh" size="md" class="animate-spin" />
    </div>
    <div v-else-if="error" class="quota-state quota-state--error">{{ loadFailedMessage }}</div>
    <div v-else-if="groups.length === 0" class="quota-state">{{ emptyMessage }}</div>

    <div v-else class="quota-groups">
      <article v-for="group in groups" :key="groupKey(group)" class="quota-group-row">
        <div class="quota-group-identity">
          <PlatformIcon :platform="platformIconValue(group.platform)" size="sm" />
          <div class="min-w-0">
            <h3>{{ group.group_name || t('admin.accounts.quotaDashboard.ungrouped') }}</h3>
            <p>
              {{ platformLabel(group.platform) }}
              <span aria-hidden="true">·</span>
              {{ t('admin.accounts.quotaDashboard.accountMeta', {
                total: group.account_count,
                active: group.active_account_count,
                schedulable: group.schedulable_account_count
              }) }}
            </p>
          </div>
        </div>

        <div v-if="group.usage_windows?.length" class="quota-windows">
          <div v-for="window in group.usage_windows" :key="window.window" class="quota-window">
            <div class="quota-window-label">
              <span>{{ windowLabel(window.window) }}</span>
              <strong>{{ formatPercent(window.average_utilization) }}</strong>
            </div>
            <div class="quota-window-track">
              <span
                :class="quotaBarClass(window.average_utilization)"
                :style="{ width: `${progressWidth(window.average_utilization)}%` }"
              />
            </div>
          </div>
        </div>

        <span :class="['quota-health', `quota-health--${groupHealth(group)}`]">
          <i />
          {{ t(`admin.accounts.quotaDashboard.groupHealth.${groupHealth(group)}`) }}
        </span>
      </article>
    </div>
  </section>
</template>

<script setup lang="ts">
import { computed } from 'vue'
import { useI18n } from 'vue-i18n'
import Icon from '@/components/icons/Icon.vue'
import PlatformIcon from '@/components/common/PlatformIcon.vue'
import { UiIconButton } from '@/ui'
import type { AccountQuotaDashboard, AccountQuotaGroupSummary, GroupPlatform } from '@/types'
import { formatDateTime } from '@/utils/format'
import { platformLabel } from '@/utils/platformColors'
import { resolveAccountQuotaGroupHealth } from '@/utils/accountQuotaHealth'

const props = defineProps<{
  dashboard: AccountQuotaDashboard | null
  loading: boolean
  error: boolean
  title: string
  emptyMessage: string
  loadFailedMessage: string
}>()
const emit = defineEmits<{ (event: 'refresh'): void }>()
const { t } = useI18n()

const totals = computed(() => props.dashboard?.totals)
const groups = computed(() => (props.dashboard?.group_summaries ?? [])
  .filter((group) => group.account_count > 0 || (group.usage_windows?.some((window) => window.account_count > 0) ?? false))
  .sort((a, b) => a.group_name.localeCompare(b.group_name)))
const formattedGeneratedAt = computed(() => props.dashboard
  ? t('admin.accounts.quotaDashboard.generatedAt', { time: formatDateTime(new Date(props.dashboard.generated_at)) })
  : '')
const metrics = computed(() => [{
  key: 'total',
  label: t('admin.accounts.quotaDashboard.totalAccounts'),
  value: totals.value?.account_count ?? 0,
  tone: '',
}, {
  key: 'schedulable',
  label: t('admin.accounts.quotaDashboard.schedulableAccounts'),
  value: totals.value?.schedulable_account_count ?? 0,
  tone: 'quota-metric--success',
}, {
  key: 'limited',
  label: t('admin.accounts.quotaDashboard.rateLimitedAccounts'),
  value: totals.value?.rate_limited_account_count ?? 0,
  tone: 'quota-metric--warning',
}, {
  key: 'error',
  label: t('admin.accounts.quotaDashboard.exceptionAccounts'),
  value: (totals.value?.error_account_count ?? 0) + (totals.value?.disabled_account_count ?? 0),
  tone: 'quota-metric--danger',
}])

function groupKey(group: AccountQuotaGroupSummary): string {
  return group.group_id ? String(group.group_id) : `${group.platform}:${group.group_name}`
}

function groupHealth(group: AccountQuotaGroupSummary) {
  return resolveAccountQuotaGroupHealth(group)
}

function platformIconValue(platform: string): GroupPlatform | undefined {
  if (['anthropic', 'openai', 'gemini', 'antigravity', 'grok', 'kiro', 'custom'].includes(platform)) {
    return platform as GroupPlatform
  }
  return undefined
}

function windowLabel(window: string): string {
  if (window === '5h') return t('admin.accounts.quotaDashboard.window5h')
  if (window === '7d') return t('admin.accounts.quotaDashboard.window7d')
  return window
}

function formatPercent(value: number): string {
  return `${(Number.isFinite(value) ? value : 0).toFixed(1)}%`
}

function progressWidth(value: number): number {
  if (!Number.isFinite(value) || value <= 0) return 0
  return Math.min(100, value)
}

function quotaBarClass(value: number): string {
  if (value >= 100) return 'quota-window-bar--danger'
  if (value >= 80) return 'quota-window-bar--warning'
  return 'quota-window-bar--success'
}
</script>

<style scoped>
.quota-summary {
  min-width: 0;
  padding: 1rem 1.125rem;
  border: 1px solid var(--ui-border);
  border-radius: var(--ui-radius-lg);
  background: var(--ui-surface);
}

.quota-summary-header {
  display: flex;
  min-width: 0;
  align-items: flex-start;
  justify-content: space-between;
  gap: 1rem;
}

.quota-summary-header h2 {
  color: var(--ui-text);
  font-size: 0.9375rem;
  font-weight: 600;
}

.quota-summary-header p {
  margin-top: 0.2rem;
  color: var(--ui-text-tertiary);
  font-size: 0.6875rem;
}

.quota-metrics {
  display: grid;
  grid-template-columns: repeat(4, minmax(0, 1fr));
  gap: 1rem;
  margin-top: 1rem;
}

.quota-metric {
  min-width: 0;
}

.quota-metric span {
  display: block;
  overflow: hidden;
  color: var(--ui-text-tertiary);
  font-size: 0.6875rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.quota-metric strong {
  display: block;
  margin-top: 0.2rem;
  color: var(--ui-text);
  font-size: 1.25rem;
  font-variant-numeric: tabular-nums;
  font-weight: 600;
}

.quota-metric .quota-metric--success {
  color: var(--ui-success);
}

.quota-metric .quota-metric--warning {
  color: var(--ui-warning);
}

.quota-metric .quota-metric--danger {
  color: var(--ui-danger);
}

.quota-state {
  padding: 1.25rem 0 0.25rem;
  color: var(--ui-text-tertiary);
  font-size: 0.75rem;
  line-height: 1.45;
}

.quota-state--error {
  color: var(--ui-danger);
}

.quota-groups {
  margin-top: 1rem;
  border-top: 1px solid var(--ui-border);
}

.quota-group-row {
  display: grid;
  grid-template-columns: minmax(12rem, 1fr) minmax(11rem, 0.9fr) auto;
  align-items: center;
  gap: 1rem;
  padding: 0.75rem 0;
}

.quota-group-row:not(:last-child) {
  border-bottom: 1px solid var(--ui-border);
}

.quota-group-identity {
  display: flex;
  min-width: 0;
  align-items: center;
  gap: 0.625rem;
}

.quota-group-identity h3 {
  overflow: hidden;
  color: var(--ui-text);
  font-size: 0.8125rem;
  font-weight: 500;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.quota-group-identity p {
  margin-top: 0.15rem;
  overflow: hidden;
  color: var(--ui-text-tertiary);
  font-size: 0.6875rem;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.quota-windows {
  display: grid;
  gap: 0.5rem;
}

.quota-window-label {
  display: flex;
  justify-content: space-between;
  gap: 0.5rem;
  color: var(--ui-text-tertiary);
  font-size: 0.6875rem;
}

.quota-window-label strong {
  color: var(--ui-text-secondary);
  font-weight: 500;
  font-variant-numeric: tabular-nums;
}

.quota-window-track {
  height: 0.25rem;
  overflow: hidden;
  margin-top: 0.25rem;
  border-radius: 999px;
  background: var(--ui-surface-hover);
}

.quota-window-track span {
  display: block;
  height: 100%;
  border-radius: inherit;
}

.quota-window-bar--success {
  background: var(--ui-success);
}

.quota-window-bar--warning {
  background: var(--ui-warning);
}

.quota-window-bar--danger {
  background: var(--ui-danger);
}

.quota-health {
  display: inline-flex;
  align-items: center;
  gap: 0.375rem;
  color: var(--ui-text-secondary);
  font-size: 0.6875rem;
  font-weight: 500;
  white-space: nowrap;
}

.quota-health i {
  width: 0.4rem;
  height: 0.4rem;
  border-radius: 50%;
  background: currentColor;
}

.quota-health--normal {
  color: var(--ui-success);
}

.quota-health--degraded,
.quota-health--constrained {
  color: var(--ui-warning);
}

.quota-health--unavailable {
  color: var(--ui-danger);
}

@media (max-width: 720px) {
  .quota-summary {
    padding: 0.875rem;
  }

  .quota-metrics {
    grid-template-columns: repeat(2, minmax(0, 1fr));
    gap: 0.75rem 1rem;
  }

  .quota-group-row {
    grid-template-columns: 1fr auto;
  }

  .quota-windows {
    grid-column: 1 / -1;
    grid-row: 2;
  }
}
</style>
