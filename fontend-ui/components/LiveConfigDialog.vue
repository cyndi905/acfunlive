<template>
  <el-dialog title="订阅主播" :visible.sync="show" width="30%">
    <div>
      <el-input v-model="inputUID" placeholder="请输入主播uid" style="width: 50%" />
      <span v-if="warn" style="color: red">请输入一个大于0的整数</span>
    </div>
    <div style="margin-top: 20px">
      <el-checkbox v-model="notify" label="开播提醒" />
      <el-checkbox v-model="record" label="自动录播" />
      <el-checkbox v-model="danmu" label="自动下载弹幕" />
      <el-checkbox v-model="danmuToDb" label="保存弹幕到数据库" />
    </div>
    <span slot="footer">
      <el-button type="primary" @click="configLive">设置</el-button>
    </span>
  </el-dialog>
</template>

<script>
export default {
  props: {
    showDialog: {
      type: Boolean,
      default: false
    }
  },
  data () {
    return {
      inputUID: '',
      notify: true,
      record: false,
      danmu: false,
      danmuToDb: false,
      warn: false,
      hostname: location.hostname
    }
  },
  computed: {
    show: {
      get () {
        return this.showDialog
      },
      set (v) {
        if (v === false) {
          this.$emit('hideDialog')
        }
      }
    }
  },
  watch: {
    // 当 danmu 被关闭时，自动关闭 danmuToDb
    danmu(newVal) {
      if (!newVal) {
        this.danmuToDb = false
      }
    },
    // 当 danmuToDb 被开启时，自动开启 danmu
    danmuToDb(newVal) {
      if (newVal) {
        this.danmu = true
      }
    }
  },
  methods: {
    async configLive () {
      if (this.inputUID === '') {
        this.show = false
        return
      }
      const uid = parseInt(this.inputUID, 10)
      if (isNaN(uid) || uid <= 0) {
        this.warn = true
        return
      }
      this.warn = false
      this.show = false

      let result = true

      const callApi = async (path) => {
        try {
          const resp = await fetch(`http://${this.hostname}:51880/${path}/${uid}`)
          return await resp.json()
        } catch (e) {
          console.error(e)
          return false
        }
      }

      if (this.notify) {
        result = result && await callApi('addnotifyon')
      }
      if (this.record) {
        result = result && await callApi('addrecord')
      }
      if (this.danmu) {
        result = result && await callApi('adddanmu')
      }
      if (this.danmuToDb) {
        result = result && await callApi('adddanmuToDb')
      }

      if (result !== true) {
        console.error(`configLive返回错误：${result}`)
      }
    }
  }
}
</script>
