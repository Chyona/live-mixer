import { capitalize, map as lodashMap, sum as lodashSum, words, isNumber } from 'lodash-es';

export function getFullName(
  filePathName: string | null,
  filePathExtension?: string | null
): string {
  return `${filePathName}${filePathExtension ? `.${filePathExtension}` : ''}`;
}

/**
 * 将连接符或者下划线变量转为驼峰格式
 * @param str
 * @returns
 */
export function toCamelCase(str: string) {
  return str.replace(/[-_](\w)/g, (match, p1) => p1.toUpperCase());
}

/**
 * 将连接符或者下划线变量转为驼峰格式并首字母大写
 * @param str
 * @returns
 */
export function ToCamelCaseCapitalize(str: string = '') {
  return toCamelCase(capitalize(str));
}

/**
 * 自定义函数将驼峰命名转换为下划线分隔
 * @param camelCaseString
 * @returns
 */
export function camelToSnake(camelCaseString: string) {
  const wordsArr = words(camelCaseString);
  const snakeCaseString = wordsArr.map((word: string) => word.toLowerCase()).join('_');
  return snakeCaseString;
}

/**
 * 将十进制拆分为移位
 * @param decimal 十进制数
 * @returns 拆分好的位转化成十进制的数组
 * 例如：10（用二进制表示是 110），拆分好的二进制分辨是 100 10，转化成十进制是 8 2，最终返回的是[8, 2]
 */
export function splitDecimalIntoShifts(decimal?: number | null) {
  const result: number[] = [];
  if (decimal === null || decimal === undefined) {
    return result;
  }
  let remaining = +decimal;

  while (remaining > 0) {
    const maxShift = Math.floor(Math.log2(remaining)) + 1;
    const powerOfTwo = 1 << (maxShift - 1);

    if (powerOfTwo <= remaining) {
      result.push(powerOfTwo);
      remaining -= powerOfTwo;
    }
  }

  return result.reverse();
}

export function mergeDecimal(decimal: number[]) {
  return lodashSum(lodashMap(decimal, Number));
}

/**
 * 将后台传入文件大小（字节）格式化成对应的大小
 * @param {string} value - 待校验数据
 * @returns {string} 返回对应大小的字符串
 */
export function formatSize(size: number, unit = 0): string {
  const DIGIT = 2; // 精确到小数点多少位
  const SCALE = 1024; // 文件大小进制
  const UNIT = ['B', 'KB', 'MB', 'GB', 'TB']; // 文件大小单位
  if (!isNumber(unit)) {
    unit = 0;
  }
  const newUnit = unit + 1;
  if (size / SCALE >= 1 && newUnit < UNIT.length) {
    return formatSize(size / SCALE, newUnit);
  }
  return (
    (size + '')
      .split('.')
      .map((v, i) => (i === 1 ? v.slice(0, DIGIT) : v))
      .join('.') +
    ' ' +
    UNIT[unit]
  );
}

export function generateUUID() {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, function (c) {
    const r = (Math.random() * 16) | 0;
    const v = c === 'x' ? r : (r & 0x3) | 0x8;
    return v.toString(16);
  });
}

interface AmountOptions {
  commas: boolean;
  currency: string;
  negativeParentheses: boolean;
  fixed: number;
}

// 金额格式化函数实现
export function formatAmount(value: number, options?: Partial<AmountOptions>) {
  // 合并默认选项
  const { commas = true, currency = '', negativeParentheses = false, fixed = 2 } = options || {};
  // 处理无效输入
  if (!value) {
    return '0.00';
  }

  // 四舍五入到两位小数
  let num = Math.round(value * 100) / 100;
  // 处理负数
  const isNegative = num < 0;
  num = Math.abs(num);
  // 格式化为 fixed 位小数
  const parts = num.toFixed(fixed).split('.');
  let integerPart = parts[0] || '';
  const decimalPart = parts[1]; // 添加千分位分隔符
  if (commas) {
    integerPart = integerPart.replace(/\B(?=(\d{3})+(?!\d))/g, ',');
  }
  // 构建结果
  let result = `${integerPart}.${decimalPart}`;
  // 添加货币符号
  if (currency) {
    result = currency + result;
  }
  // 处理负数
  if (isNegative) {
    if (negativeParentheses) {
      result = `(${result})`;
    } else {
      result = '-' + result;
    }
  }
  return result;
}

export function createParticles() {
  const particleCount = 15;
  return Array.from({ length: particleCount }).map((item, index) => {
    // 随机大小
    const size = 2 + Math.random() * 4;
    const sizeStyle = `${size}px`;
    const pStyle = {
      width: sizeStyle,
      height: sizeStyle,
      left: `${Math.random() * 100}%`,
      animationDelay: `${Math.random() * 8}s`,
      animationDuration: `${5 + Math.random() * 5}s`, // 随机动画时长
    };

    return <div className="particle" style={pStyle} key={index}></div>;
  });
}

// 辅助函数：将十六进制颜色转换为RGB
export const hexToRgb = (hex: string) => {
  const result = /^#?([a-f\d]{2})([a-f\d]{2})([a-f\d]{2})$/i.exec(hex);
  if (!result) {
    return '79, 172, 254';
  }
  const [, r, g, b] = result;
  return `${parseInt(r ?? '0', 16)}, ${parseInt(g ?? '0', 16)}, ${parseInt(b ?? '0', 16)}`;
};

// 用于判断是否是移动设备
export const isMobile = (): boolean => {
  return /Android|webOS|iPhone|iPad|iPod|BlackBerry|IEMobile|Opera Mini/i.test(navigator.userAgent);
};

// 区分手机和平板
export const isPhone = (): boolean => {
  const userAgent = navigator.userAgent;

  // 排除 iPad 和一些大屏设备
  if (/iPad/i.test(userAgent)) return false;

  // 判断是否是移动设备
  return /Android|webOS|iPhone|iPod|BlackBerry|IEMobile|Opera Mini/i.test(userAgent);
};
