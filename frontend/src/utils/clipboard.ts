export interface CopyTextOptions {
  writeText?: (text: string) => Promise<void>
  legacyCopy?: (text: string) => boolean
  promptManualCopy?: (text: string) => void
}

function defaultWriteText(text: string) {
  const clipboard = globalThis.navigator?.clipboard
  if (!clipboard?.writeText) {
    throw new Error('Clipboard API unavailable')
  }
  return clipboard.writeText(text)
}

function defaultLegacyCopy(text: string) {
  const doc = globalThis.document
  if (!doc?.body || typeof doc.createElement !== 'function' || typeof doc.execCommand !== 'function') {
    return false
  }

  const textarea = doc.createElement('textarea')
  textarea.value = text
  textarea.readOnly = true
  textarea.setAttribute('aria-hidden', 'true')
  textarea.style.position = 'fixed'
  textarea.style.top = '0'
  textarea.style.left = '-9999px'
  textarea.style.opacity = '0'
  textarea.style.pointerEvents = 'none'
  doc.body.appendChild(textarea)
  textarea.focus()
  textarea.select()
  textarea.setSelectionRange(0, textarea.value.length)

  let copied = false
  try {
    copied = doc.execCommand('copy')
  } catch {
    copied = false
  } finally {
    textarea.remove()
  }
  return copied
}

function defaultPromptManualCopy(text: string) {
  const promptFn = globalThis.window?.prompt
  if (promptFn) {
    promptFn('复制失败，请手动复制下面的内容', text)
    return
  }
  globalThis.window?.alert?.(`复制失败，请手动复制下面的内容:\n${text}`)
}

export async function copyText(text: string, options: CopyTextOptions = {}) {
  try {
    await (options.writeText ?? defaultWriteText)(text)
    return true
  } catch {
    try {
      if ((options.legacyCopy ?? defaultLegacyCopy)(text)) {
        return true
      }
    } catch {
      // Fall through to the manual prompt.
    }
    ;(options.promptManualCopy ?? defaultPromptManualCopy)(text)
    return false
  }
}
