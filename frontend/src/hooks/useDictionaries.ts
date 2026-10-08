import { useEffect, useState } from 'react'
import { metaApi, type DictItem } from '../api/meta'

/**
 * 字典目录：码值 → 中文名一律取 /api/meta/dictionaries（M1 数据字典的只读投影），
 * 界面不保留副本 —— 字典改动即随接口生效、不需要重新发版（与姊妹仓 useParamFace 同形态，DEF-09 口径）。
 *
 * tone（配色档位）不是字典维度，仍由界面本地决定；接口取不到时下拉为空、胶囊回吐码值，
 * 比按本地旧副本给出已被治理拒绝的选项更安全。
 */
export function useDictionaries() {
  const [dicts, setDicts] = useState<Record<string, DictItem[]>>({})
  const [loaded, setLoaded] = useState(false)

  useEffect(() => {
    let alive = true
    metaApi
      .dictionaries()
      .then((d) => {
        if (!alive) return
        setDicts(d || {})
        setLoaded(true)
      })
      .catch(() => {
        // 拦截器已直出后端中文提示
      })
    return () => {
      alive = false
    }
  }, [])

  const itemsOf = (dict: string) => dicts[dict] || []
  const optionsOf = (dict: string) => itemsOf(dict).map((i) => ({ value: i.code, label: i.label }))
  const labelOf = (dict: string, code: string | null | undefined) => {
    const v = code || ''
    return itemsOf(dict).find((i) => i.code === v)?.label || v || '-'
  }

  return { dicts, loaded, itemsOf, optionsOf, labelOf }
}
