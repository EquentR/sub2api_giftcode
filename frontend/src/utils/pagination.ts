export interface PaginatedItems<T> {
  items: T[]
  total: number
  page: number
  pageSize: number
  pages: number
}

export function paginateItems<T>(items: T[], page: number, pageSize: number): PaginatedItems<T> {
  const total = items.length
  const safePageSize = Math.max(1, Math.floor(pageSize))
  const pages = Math.max(1, Math.ceil(total / safePageSize))
  const safePage = Math.min(Math.max(1, Math.floor(page)), pages)
  const start = (safePage - 1) * safePageSize

  return {
    items: items.slice(start, start + safePageSize),
    total,
    page: safePage,
    pageSize: safePageSize,
    pages,
  }
}
