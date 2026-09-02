<template>
  <BaseDialog
    :show="show"
    :title="t('admin.compliance.title')"
    width="wide"
    :close-on-escape="false"
    :close-on-click-outside="false"
    @close="handleCancel"
  >
    <div class="space-y-4">
      <p class="text-sm text-gray-600 dark:text-gray-400">
        {{ t('admin.compliance.description') }}
      </p>

      <div class="space-y-2 rounded-xl border border-gray-200 p-4 dark:border-dark-700">
        <a
          :href="status.document_url_zh"
          target="_blank"
          rel="noopener noreferrer"
          class="inline-flex items-center gap-1 text-sm font-medium text-primary-600 hover:underline dark:text-primary-400"
        >
          {{ t('admin.compliance.documentZh') }}
        </a>
        <a
          :href="status.document_url_en"
          target="_blank"
          rel="noopener noreferrer"
          class="inline-flex items-center gap-1 text-sm font-medium text-primary-600 hover:underline dark:text-primary-400"
        >
          {{ t('admin.compliance.documentEn') }}
        </a>
        <p class="pt-1 text-xs text-gray-400">
          {{ t('admin.compliance.version') }}: {{ status.version }}
        </p>
      </div>

      <div>
        <label class="input-label">
          {{ t('admin.compliance.phraseLabel') }}
        </label>
        <input
          v-model="phrase"
          type="text"
          class="input"
          :placeholder="status.ack_phrase_zh"
          :disabled="submitting"
        />
        <p class="input-hint">
          {{ t('admin.compliance.phraseHint') }}
        </p>
      </div>

      <p v-if="error" class="text-sm text-red-500">{{ error }}</p>
    </div>

    <template #footer>
      <div class="flex justify-end space-x-3">
        <button
          type="button"
          class="btn btn-secondary"
          :disabled="submitting"
          @click="handleCancel"
        >
          {{ t('common.logout') }}
        </button>
        <button
          type="button"
          class="btn btn-primary"
          :disabled="submitting || !phrase.trim()"
          @click="handleAccept"
        >
          {{ submitting ? t('admin.compliance.submitting') : t('admin.compliance.accept') }}
        </button>
      </div>
    </template>
  </BaseDialog>
</template>

<script setup lang="ts">
import { ref, watch } from 'vue'
import { useI18n } from 'vue-i18n'
import BaseDialog from '@/components/common/BaseDialog.vue'
import {
  getAdminComplianceStatus,
  acceptAdminCompliance,
  type AdminComplianceStatus,
} from '@/api/admin/compliance'

const props = defineProps<{
  show: boolean
}>()

const emit = defineEmits<{
  (e: 'close'): void
  (e: 'accepted'): void
}>()

const { t } = useI18n()

const status = ref<AdminComplianceStatus>({
  required: true,
  version: '',
  document_path_zh: '',
  document_path_en: '',
  document_url_zh: '',
  document_url_en: '',
  ack_phrase_zh: '',
  ack_phrase_en: '',
})
const phrase = ref('')
const submitting = ref(false)
const error = ref('')

watch(
  () => props.show,
  async (show) => {
    if (show) {
      error.value = ''
      try {
        const s = await getAdminComplianceStatus()
        if (s) status.value = s
      } catch {
        // status fetch failure is non-fatal; defaults remain
      }
    }
  }
)

function handleCancel() {
  if (submitting.value) return
  emit('close')
}

async function handleAccept() {
  if (!phrase.value.trim() || submitting.value) return
  submitting.value = true
  error.value = ''
  try {
    await acceptAdminCompliance(phrase.value.trim())
    emit('accepted')
    emit('close')
  } catch (e: unknown) {
    const err = e as { message?: string }
    error.value = err.message || t('admin.compliance.acceptFailed')
  } finally {
    submitting.value = false
  }
}
</script>
