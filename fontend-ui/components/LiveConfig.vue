<template>
  <div style="position: relative; top: 8px;">
    <el-checkbox v-model="notify" label="开播提醒" @change="changeNotify" />
    <el-checkbox v-model="record" label="自动录播" @change="changeRecord" />
    <el-checkbox v-model="danmu" label="自动下载弹幕" @change="changeDanmu" />
    <el-checkbox v-model="danmuToDb" label="保存弹幕到数据库" @change="changeDanmuToDb" />
    <el-popconfirm icon="el-icon-info" icon-color="red" :title="'确定删除 ' + config.name + '（' + config.uid + '） 的设置？'"
      @confirm="deleteLive">
      <el-button slot="reference" size="small" style="position: absolute; right: 30px;">
        删除主播
      </el-button>
    </el-popconfirm>
  </div>
</template>

<script>
export default {
  props: {
    config: {
      type: Object,
      default: null
    },
    stopRec: {
      type: Function,
      default: null
    }
  },
  data() {
    return {
      notify: this.config.notify?.notifyOn || false,
      record: this.config.record || false,
      danmu: this.config.danmu || false,
      danmuToDb: this.config.danmuToDb || false, // 新增
      hostname: location.hostname
    }
  },
  watch: {
    config(val) {
      if (val) {
        const normalized = this.normalizeDanmuState(val)
        this.danmu = normalized.danmu
        this.danmuToDb = normalized.danmuToDb
        this.notify = val.notify?.notifyOn || false
        this.record = val.record || false
      }
    },
    // 联动逻辑：前端保持与后端一致
    danmu(newVal) {
      if (!newVal) {
        // 关闭 danmu 时，自动关闭 danmuToDb
        this.danmuToDb = false
      }
    },
    danmuToDb(newVal) {
      if (newVal) {
        // 开启 danmuToDb 时，自动开启 danmu
        this.danmu = true
      }
    }
  },
  methods: {
    normalizeDanmuState(config) {
      const danmu = config?.danmu || false;
      // 只有 danmu 为 true 时，danmuToDb 才能为 true
      const danmuToDb = danmu && (config?.danmuToDb || false);
      return { danmu, danmuToDb };
    },
    async changeNotify(checked) {
      const endpoint = checked ? 'addnotifyon' : 'delnotifyon'
      const result = await this.callApi(endpoint)
      if (result !== true) console.error(`changeNotify返回错误：${result}`)
    },
    async changeRecord(checked) {
      const endpoint = checked ? 'addrecord' : 'delrecord'
      const result = await this.callApi(endpoint)
      if (result !== true) console.error(`changeRecord返回错误：${result}`)
    },
    async changeDanmu(checked) {
      const endpoint = checked ? 'adddanmu' : 'deldanmu'
      const result = await this.callApi(endpoint)
      if (result !== true) console.error(`changeDanmu返回错误：${result}`)
    },
    async changeDanmuToDb(checked) {
      const endpoint = checked ? 'adddanmuToDb' : 'deldanmuToDb'
      const result = await this.callApi(endpoint)
      if (result !== true) console.error(`changeDanmuToDb返回错误：${result}`)
    },
    async callApi(path) {
      try {
        const resp = await fetch(`http://${this.hostname}:51880/${path}/${this.config.uid}`)
        return await resp.json()
      } catch (e) {
        console.error(e)
        return false
      }
    },
    async deleteLive() {
      if (this.config.isRecord) {
        this.stopRec(this.config.uid)
      }
      const result = await fetch(`http://${this.hostname}:51880/delconfig/${this.config.uid}`)
        .then(resp => resp.json())
        .catch(e => console.error(e))
      if (result !== true) {
        console.error(`deleteLive返回错误：${result}`)
      }
    }
  }
}
</script>
