export function errorMessage(err: unknown): string {
  if (typeof err === 'string' && err.trim() !== '') {
    return err
  }
  if (err instanceof Error && err.message.trim() !== '') {
    return err.message
  }
  return '操作失败，请稍后重试'
}

export function isServiceNotReady(err: unknown): boolean {
  const msg = errorMessage(err)
  return msg.includes('请先点击') || msg.includes('后端尚未') || msg.includes('尚未初始化')
}
