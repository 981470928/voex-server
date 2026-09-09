module.exports = {
  apps: [
    {
      name: 'file-server',
      script: 'src/index.ts',
      interpreter: 'node_modules/.bin/tsx',
      cwd: __dirname,
      env: {
        NODE_ENV: 'production',
      },
      max_memory_restart: '256M',
      log_date_format: 'YYYY-MM-DD HH:mm:ss',
      error_file: 'logs/error.log',
      out_file: 'logs/out.log',
      merge_logs: true,
    },
  ],
};
