import { useState, type FormEvent } from 'react'

import { Button } from '@/ui-kit'

import { apiErrorText } from '../../errorText'
import {
  OPTION_COLOR_PALETTE,
  nextOptionColor,
  type FieldOptionValue,
} from '../../types/field-config'
import styles from './FieldPanels.module.css'

export interface OptionsEditorProps {
  configMap: Record<string, unknown>
  onSave: (options: FieldOptionValue[], allowCreate: boolean) => Promise<void>
  onClose: () => void
}

/** 从字段 config_map 读取选项列表（颜色缺失时按调色板顺序补足）。 */
export function readOptionList(configMap: Record<string, unknown>): FieldOptionValue[] {
  const raw = configMap.option_list
  if (!Array.isArray(raw)) return []
  return raw.map((entry, index) => {
    const record = typeof entry === 'object' && entry !== null ? (entry as Record<string, unknown>) : {}
    const color =
      typeof record.color === 'string' && record.color !== ''
        ? record.color
        : nextOptionColor(
            raw
              .slice(0, index)
              .map((item) =>
                typeof item === 'object' && item !== null
                  ? String((item as Record<string, unknown>).color ?? '')
                  : '',
              ),
          )
    return {
      id: typeof record.id === 'string' ? record.id : '',
      name: typeof record.name === 'string' ? record.name : '',
      color,
    }
  })
}

/** 选项维护：增删改、上下移动/拖拽排序、调色板自动分配与改色、已用选项二次确认、allow_create 开关。 */
export function OptionsEditor({ configMap, onSave, onClose }: OptionsEditorProps) {
  const [options, setOptions] = useState<FieldOptionValue[]>(() => readOptionList(configMap))
  const [allowCreate, setAllowCreate] = useState<boolean>(
    typeof configMap.allow_create === 'boolean' ? configMap.allow_create : false,
  )
  const [newName, setNewName] = useState('')
  const [error, setError] = useState<string | null>(null)
  const [pendingDelete, setPendingDelete] = useState<number | null>(null)
  const [dragIndex, setDragIndex] = useState<number | null>(null)
  const [pending, setPending] = useState(false)

  function updateName(index: number, name: string) {
    setOptions((current) => current.map((option, i) => (i === index ? { ...option, name } : option)))
    setError(null)
  }

  function updateColor(index: number, color: string) {
    setOptions((current) => current.map((option, i) => (i === index ? { ...option, color } : option)))
  }

  function move(from: number, to: number) {
    if (to < 0 || to >= options.length) return
    setOptions((current) => {
      const next = [...current]
      const [item] = next.splice(from, 1)
      next.splice(to, 0, item)
      return next
    })
  }

  function addOption() {
    const name = newName.trim()
    if (name === '') {
      setError('选项名称不可为空')
      return
    }
    if (options.some((option) => option.name.trim().toLocaleLowerCase() === name.toLocaleLowerCase())) {
      setError('选项名称不可重复')
      return
    }
    setOptions((current) => [
      ...current,
      { id: '', name, color: nextOptionColor(current.map((option) => option.color)) },
    ])
    setNewName('')
    setError(null)
  }

  function requestDelete(index: number) {
    const option = options[index]
    if (option.id === '') {
      setOptions((current) => current.filter((_, i) => i !== index))
      return
    }
    setPendingDelete(index)
  }

  function confirmDelete() {
    if (pendingDelete === null) return
    setOptions((current) => current.filter((_, i) => i !== pendingDelete))
    setPendingDelete(null)
  }

  function validate(): string | null {
    if (options.length === 0) return null
    const names = new Set<string>()
    for (const option of options) {
      const name = option.name.trim()
      if (name === '') return '选项名称不可为空'
      const key = name.toLocaleLowerCase()
      if (names.has(key)) return '选项名称不可重复'
      names.add(key)
    }
    return null
  }

  function submit(event: FormEvent) {
    event.preventDefault()
    const message = validate()
    if (message !== null) {
      setError(message)
      return
    }
    setError(null)
    setPending(true)
    onSave(
      options.map((option) => ({ ...option, name: option.name.trim() })),
      allowCreate,
    )
      .then(onClose)
      .catch((failure: unknown) => setError(apiErrorText(failure)))
      .finally(() => setPending(false))
  }

  return (
    <form className={styles.form} onSubmit={submit}>
      <label className={styles.checkboxRow} htmlFor="options-allow-create">
        <input
          id="options-allow-create"
          type="checkbox"
          checked={allowCreate}
          onChange={(event) => setAllowCreate(event.target.checked)}
        />
        允许录入时添加新选项
      </label>

      {options.length === 0 ? (
        <p className={styles.hint}>暂无选项，添加后即可在单元格中选择。</p>
      ) : (
        <ul className={styles.optionList}>
          {options.map((option, index) => (
            <li
              key={`${option.id}-${index}`}
              className={styles.optionRow}
              draggable
              onDragStart={() => setDragIndex(index)}
              onDragOver={(event) => event.preventDefault()}
              onDrop={() => {
                if (dragIndex !== null) move(dragIndex, index)
                setDragIndex(null)
              }}
            >
              <input
                aria-label={`选项名称：${option.name}`}
                value={option.name}
                onChange={(event) => updateName(index, event.target.value)}
              />
              <select
                aria-label={`选项颜色：${option.name}`}
                value={option.color}
                onChange={(event) => updateColor(index, event.target.value)}
              >
                {OPTION_COLOR_PALETTE.map((color) => (
                  <option key={color} value={color}>
                    {color}
                  </option>
                ))}
              </select>
              <div className={styles.rowActions}>
                <button
                  type="button"
                  className={styles.smallButton}
                  aria-label={`上移：${option.name}`}
                  disabled={index === 0}
                  onClick={() => move(index, index - 1)}
                >
                  上移
                </button>
                <button
                  type="button"
                  className={styles.smallButton}
                  aria-label={`下移：${option.name}`}
                  disabled={index === options.length - 1}
                  onClick={() => move(index, index + 1)}
                >
                  下移
                </button>
                <button
                  type="button"
                  className={styles.smallButton}
                  aria-label={`删除选项：${option.name}`}
                  onClick={() => requestDelete(index)}
                >
                  删除
                </button>
              </div>
              {pendingDelete === index ? (
                <div className={styles.confirmBox}>
                  <p>「{option.name}」已被记录使用，删除后对应记录值将清空。</p>
                  <div className={styles.rowActions}>
                    <button type="button" className={styles.smallButton} onClick={confirmDelete}>
                      确认删除
                    </button>
                    <button
                      type="button"
                      className={styles.smallButton}
                      onClick={() => setPendingDelete(null)}
                    >
                      取消
                    </button>
                  </div>
                </div>
              ) : null}
            </li>
          ))}
        </ul>
      )}

      <label htmlFor="option-new-name">新选项名称</label>
      <input
        id="option-new-name"
        value={newName}
        onChange={(event) => {
          setNewName(event.target.value)
          setError(null)
        }}
      />
      <button type="button" className={styles.smallButton} onClick={addOption}>
        添加选项
      </button>

      {error === null ? null : <p role="alert">{error}</p>}
      <div className={styles.dialogActions}>
        <Button type="submit" disabled={pending}>
          保存
        </Button>
        <Button variant="secondary" onClick={onClose}>
          关闭
        </Button>
      </div>
    </form>
  )
}
