const MESSAGE_MAP: Record<string, string> = {
  unauthorized: '未授权，或会话已失效',
  forbidden: '没有权限',
  'not found': '未找到资源',
  conflict: '请求冲突，请刷新后重试',
  'bad request': '请求参数错误',
  'internal server error': '服务器内部错误',
  'upstream failed': '上游服务请求失败',
  'request failed': '请求失败',
  'network error': '网络错误，请稍后重试',
}

export function translateMessage(message?: string) {
  const text = message?.trim()
  if (!text) {
    return '请求失败'
  }
  const translated = MESSAGE_MAP[text.toLowerCase()]
  return translated ?? text
}
